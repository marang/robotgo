package agent

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	robotgo "github.com/marang/robotgo"
)

const (
	UISchemaVersion                 = "1"
	maxUIBackendReferenceBytes      = 4 * 1024
	maxUIBackendReferenceTotalBytes = 1 << 20
	maxUIStatesPerNode              = 9
	maxUIActionsPerNode             = 8
	uiCompletionAuditTimeout        = time.Second
)

// UIRole is the bounded, platform-neutral semantic role vocabulary exposed to
// agents. Backend-specific roles never cross the agent boundary.
type UIRole string

const (
	UIRoleApplication UIRole = "application"
	UIRoleWindow      UIRole = "window"
	UIRoleDialog      UIRole = "dialog"
	UIRoleButton      UIRole = "button"
	UIRoleCheckbox    UIRole = "checkbox"
	UIRoleRadio       UIRole = "radio"
	UIRoleTextBox     UIRole = "textbox"
	UIRolePassword    UIRole = "password"
	UIRoleLabel       UIRole = "label"
	UIRoleLink        UIRole = "link"
	UIRoleList        UIRole = "list"
	UIRoleListItem    UIRole = "list-item"
	UIRoleMenu        UIRole = "menu"
	UIRoleMenuItem    UIRole = "menu-item"
	UIRoleTab         UIRole = "tab"
	UIRoleTabPanel    UIRole = "tab-panel"
	UIRoleSlider      UIRole = "slider"
	UIRoleProgress    UIRole = "progress"
	UIRoleImage       UIRole = "image"
	UIRoleTable       UIRole = "table"
	UIRoleRow         UIRole = "row"
	UIRoleCell        UIRole = "cell"
	UIRoleGroup       UIRole = "group"
	UIRoleGeneric     UIRole = "generic"
)

// UIProperty identifies one semantic field that policy permits to cross MCP.
type UIProperty string

const (
	UIPropertyRole        UIProperty = "role"
	UIPropertyName        UIProperty = "name"
	UIPropertyDescription UIProperty = "description"
	UIPropertyValue       UIProperty = "value"
	UIPropertyState       UIProperty = "state"
	UIPropertyBounds      UIProperty = "bounds"
	UIPropertyFocus       UIProperty = "focus"
	UIPropertyActions     UIProperty = "actions"
	UIPropertyHierarchy   UIProperty = "hierarchy"
)

// UIState and UIAction are fixed vocabularies. Native backend text can never
// be smuggled through these structural fields.
type UIState string
type UIAction string

const (
	UIStateEnabled   UIState = "enabled"
	UIStateDisabled  UIState = "disabled"
	UIStateChecked   UIState = "checked"
	UIStateSelected  UIState = "selected"
	UIStateExpanded  UIState = "expanded"
	UIStateCollapsed UIState = "collapsed"
	UIStateReadOnly  UIState = "read-only"
	UIStateRequired  UIState = "required"
	UIStateInvalid   UIState = "invalid"

	UIActionPress     UIAction = "press"
	UIActionFocus     UIAction = "focus"
	UIActionSetValue  UIAction = "set-value"
	UIActionToggle    UIAction = "toggle"
	UIActionExpand    UIAction = "expand"
	UIActionCollapse  UIAction = "collapse"
	UIActionIncrement UIAction = "increment"
	UIActionDecrement UIAction = "decrement"
)

// UIBounds is one bounded global logical rectangle.
type UIBounds struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

// UIElement is a privacy-reduced semantic element. ElementID is opaque and is
// meaningful only together with ObservationID from the containing result.
type UIElement struct {
	ElementID     string     `json:"element_id"`
	Role          UIRole     `json:"role"`
	Name          string     `json:"name,omitempty"`
	Description   string     `json:"description,omitempty"`
	Value         string     `json:"value,omitempty"`
	ValueRedacted bool       `json:"value_redacted,omitempty"`
	Sensitive     bool       `json:"sensitive,omitempty"`
	States        []UIState  `json:"states,omitempty"`
	Bounds        *UIBounds  `json:"bounds,omitempty"`
	Focused       bool       `json:"focused,omitempty"`
	Actions       []UIAction `json:"actions,omitempty"`
	ParentID      string     `json:"parent_id,omitempty"`
	ChildIDs      []string   `json:"child_ids,omitempty"`
}

// UIObservation is a bounded semantic snapshot. It contains no native object
// handles, backend object paths, hidden nodes, password values, or raw errors.
type UIObservation struct {
	SchemaVersion string      `json:"schema_version"`
	ObservationID string      `json:"observation_id"`
	CreatedAt     time.Time   `json:"created_at"`
	Backend       string      `json:"backend"`
	Truncated     bool        `json:"truncated"`
	Elements      []UIElement `json:"elements"`
}

// InspectUIRequest selects exactly one immutable policy-approved window.
type InspectUIRequest struct {
	Target    int              `json:"target"`
	Kind      WindowTargetKind `json:"kind"`
	Confirmed bool             `json:"confirmed,omitempty"`
}

type uiBackendLimits struct {
	MaxElements            uint32
	MaxDepth               uint32
	MaxStringBytes         uint32
	MaxReferenceBytes      uint32
	MaxTotalReferenceBytes uint32
}

type uiBackendNode struct {
	StableID    []byte
	Parent      int
	Depth       uint32
	Role        UIRole
	Name        string
	Description string
	Value       string
	Sensitive   bool
	Hidden      bool
	Offscreen   bool
	States      []UIState
	Bounds      *UIBounds
	Focused     bool
	Actions     []UIAction
}

type uiBackendSnapshot struct {
	Backend   string
	Nodes     []uiBackendNode
	Truncated bool
}

type uiInspectDriver interface {
	InspectUI(context.Context, int, uiBackendLimits) (uiBackendSnapshot, error)
}

func (robotGoDriver) InspectUI(context.Context, int, uiBackendLimits) (uiBackendSnapshot, error) {
	return uiBackendSnapshot{}, fmt.Errorf("%w: no native accessibility adapter is active", robotgo.ErrNotSupported)
}

// InspectUI returns one policy-scoped semantic tree. It never opens a desktop
// permission prompt and consumes both query and observation quota on attempt.
func (s *Session) InspectUI(ctx context.Context, request InspectUIRequest) (UIObservation, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	result := UIObservation{SchemaVersion: UISchemaVersion, CreatedAt: time.Now().UTC()}
	if request.Target <= 0 || !validWindowTargetKind(request.Kind) {
		return result, uiError(ErrorInvalidInput, "invalid UI inspection target", nil)
	}
	if err := s.acquire(ctx); err != nil {
		return result, uiOperationError(err)
	}
	defer s.release()
	if err := s.ensureOpen(); err != nil {
		return result, uiOperationError(err)
	}
	if err := s.authorizeUIInspection(request); err != nil {
		return result, err
	}
	capability, ok := s.capability(OperationInspectUI)
	if !ok || !capability.Available || capability.Backend == "" {
		cause := robotgo.ErrNotSupported
		if ok && capability.UnavailableCode == ErrorPermissionDenied {
			cause = robotgo.ErrPermissionDenied
		}
		code := ErrorUnsupported
		if ok && capability.UnavailableCode != "" {
			code = capability.UnavailableCode
		}
		return result, uiError(code, "semantic UI inspection is unavailable", cause)
	}
	if s.usedQueries >= s.policy.MaxQueries || s.usedObservations >= s.policy.MaxObservations {
		return result, uiError(ErrorPolicyDenied, "agent policy UI inspection quota reached", ErrPolicyDenied)
	}
	if err := s.emitAudit(ctx, AuditEvent{Kind: AuditObservationStarted, Operation: OperationInspectUI}); err != nil {
		return result, uiError(ErrorAuditDelivery, "audit sink rejected UI inspection intent", err)
	}
	inspectCtx, cancel := context.WithCancel(ctx)
	stopSessionCancel := context.AfterFunc(s.ctx, cancel)
	defer func() {
		stopSessionCancel()
		cancel()
	}()
	if err := s.uiExecutionError(ctx); err != nil {
		return s.finishUIInspectionFailure(ctx, result, err)
	}
	if err := s.beginUIQuery(); err != nil {
		return s.finishUIInspectionFailure(ctx, result,
			uiError(ErrorPolicyDenied, "agent policy UI inspection rate limit reached", err))
	}
	handle, err := s.resolveValidatedWindow(ActivateWindowAction{Target: request.Target, Kind: request.Kind})
	if err != nil {
		code, message := classifyBackendError(err)
		if errors.Is(err, ErrStaleTarget) {
			code, message = ErrorStaleTarget, "UI inspection target is stale"
		}
		return s.finishUIInspectionFailure(ctx, result, uiError(code, message, err))
	}
	if err := s.uiExecutionError(ctx); err != nil {
		return s.finishUIInspectionFailure(ctx, result, err)
	}
	driver, ok := s.driver.(uiInspectDriver)
	if !ok {
		return s.finishUIInspectionFailure(ctx, result,
			uiError(ErrorUnsupported, "semantic UI inspection is unsupported", robotgo.ErrNotSupported))
	}
	snapshot, err := driver.InspectUI(inspectCtx, handle, uiBackendLimits{
		MaxElements: s.policy.MaxUIElements, MaxDepth: s.policy.MaxUITreeDepth,
		MaxStringBytes:         s.policy.MaxUIStringBytes,
		MaxReferenceBytes:      maxUIBackendReferenceBytes,
		MaxTotalReferenceBytes: maxUIBackendReferenceTotalBytes,
	})
	defer clearUIBackendSnapshot(&snapshot)
	if err != nil {
		if executionErr := s.uiExecutionError(ctx); executionErr != nil {
			return s.finishUIInspectionFailure(ctx, result, executionErr)
		}
		code, message := classifyBackendError(err)
		return s.finishUIInspectionFailure(ctx, result, uiError(code, message, err))
	}
	if err := s.uiExecutionError(ctx); err != nil {
		return s.finishUIInspectionFailure(ctx, result, err)
	}
	result.ObservationID = newObservationID()
	result.Backend = capability.Backend
	if result.Backend == "" || snapshot.Backend != result.Backend {
		empty := emptyUIObservation(result.CreatedAt)
		return s.finishUIInspectionFailure(ctx, empty,
			uiError(ErrorBackendFailure, "accessibility backend returned an invalid tree", errors.New("accessibility backend identity mismatch")))
	}
	elements, references, truncated, err := sanitizeUIBackendSnapshot(result.ObservationID, snapshot, s.policy)
	if err != nil {
		closeUIReferences(references)
		empty := emptyUIObservation(result.CreatedAt)
		return s.finishUIInspectionFailure(ctx, empty,
			uiError(ErrorBackendFailure, "accessibility backend returned an invalid tree", err))
	}
	if err := s.uiExecutionError(ctx); err != nil {
		closeUIReferences(references)
		return s.finishUIInspectionFailure(ctx, emptyUIObservation(result.CreatedAt), err)
	}
	result.Elements = elements
	result.Truncated = snapshot.Truncated || truncated
	s.storeUIObservation(result.ObservationID, references)
	if err := s.emitAudit(ctx, AuditEvent{
		Kind: AuditObservationFinished, Operation: OperationInspectUI,
		ObservationID: result.ObservationID,
	}); err != nil {
		return result, uiError(ErrorAuditDelivery, "UI inspection completed but audit delivery failed", err)
	}
	return result, nil
}

func (s *Session) beginUIQuery() error {
	now := s.now()
	if !s.lastUIQuery.IsZero() {
		elapsed := now.Sub(s.lastUIQuery)
		minimum := time.Duration(s.policy.MinUIQueryIntervalMillis) * time.Millisecond
		if elapsed < 0 || elapsed < minimum {
			return ErrPolicyDenied
		}
	}
	s.usedQueries++
	s.usedObservations++
	s.lastUIQuery = now
	return nil
}

func (s *Session) uiExecutionError(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return uiContextError(ctx)
	}
	if err := s.ctx.Err(); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return uiError(ErrorTimedOut, "agent session lifetime expired", context.DeadlineExceeded)
		}
		return uiError(ErrorSessionClosed, "agent session is closed", ErrSessionClosed)
	}
	return nil
}

func (s *Session) finishUIInspectionFailure(
	ctx context.Context,
	result UIObservation,
	operationErr error,
) (UIObservation, error) {
	auditCtx := ctx
	cancel := func() {}
	if ctx.Err() != nil {
		auditCtx, cancel = context.WithTimeout(context.Background(), uiCompletionAuditTimeout)
	}
	defer cancel()
	if auditErr := s.emitAudit(auditCtx, AuditEvent{
		Kind:      AuditObservationFinished,
		Operation: OperationInspectUI,
		ErrorCode: classifyUIInspectionError(operationErr),
	}); auditErr != nil {
		return result, uiError(
			ErrorAuditDelivery,
			"UI inspection failed and audit delivery failed",
			errors.Join(operationErr, auditErr),
		)
	}
	return result, operationErr
}

func classifyUIInspectionError(err error) ErrorCode {
	var actionErr *ActionError
	if errors.As(err, &actionErr) {
		return actionErr.Code
	}
	code, _ := classifyBackendError(err)
	return code
}

func emptyUIObservation(createdAt time.Time) UIObservation {
	return UIObservation{SchemaVersion: UISchemaVersion, CreatedAt: createdAt}
}

func (s *Session) authorizeUIInspection(request InspectUIRequest) error {
	if _, allowed := s.policy.allowOperation[OperationInspectUI]; !allowed {
		return uiError(ErrorPolicyDenied, "agent policy denied semantic UI inspection", ErrPolicyDenied)
	}
	if _, required := s.policy.requireConfirmation[OperationInspectUI]; required && !request.Confirmed {
		return uiError(ErrorPolicyDenied, "agent policy requires UI inspection confirmation", ErrPolicyDenied)
	}
	identity := windowTargetIdentity{target: request.Target, kind: request.Kind}
	if _, allowed := s.policy.allowWindow[identity]; !allowed {
		return uiError(ErrorPolicyDenied, "agent policy denied the UI inspection target", ErrPolicyDenied)
	}
	return nil
}

func sanitizeUIBackendSnapshot(observationID string, snapshot uiBackendSnapshot, policy Policy) ([]UIElement, map[string][]byte, bool, error) {
	if snapshot.Backend == "" {
		return nil, nil, false, errors.New("empty accessibility backend name")
	}
	if len(snapshot.Nodes) > maxAgentUIElements {
		return nil, nil, false, errors.New("accessibility tree exceeds the hard node limit")
	}
	limit := int(policy.MaxUIElements)
	if limit > len(snapshot.Nodes) {
		limit = len(snapshot.Nodes)
	}
	elements := make([]UIElement, 0, limit)
	references := make(map[string][]byte, limit)
	indexIDs := make(map[int]string, limit)
	stableIDs := make(map[[sha256.Size]byte]struct{}, limit)
	remaining := int(policy.MaxUIStringBytes)
	remainingReferences := maxUIBackendReferenceTotalBytes
	rawStringBytes := 0
	truncated := len(snapshot.Nodes) > limit

	for index, node := range snapshot.Nodes {
		if index >= limit {
			break
		}
		if len(node.StableID) == 0 || len(node.StableID) > maxUIBackendReferenceBytes ||
			len(node.StableID) > remainingReferences || node.Depth > policy.MaxUITreeDepth ||
			!validUIRole(node.Role) {
			closeUIReferences(references)
			return nil, nil, false, errors.New("invalid accessibility node identity, depth, or role")
		}
		remainingReferences -= len(node.StableID)
		if node.Parent < -1 || node.Parent >= index ||
			(node.Parent == -1 && node.Depth != 0) ||
			(node.Parent >= 0 && snapshot.Nodes[node.Parent].Depth+1 != node.Depth) {
			closeUIReferences(references)
			return nil, nil, false, errors.New("invalid accessibility hierarchy")
		}
		stableKey := sha256.Sum256(node.StableID)
		if _, duplicate := stableIDs[stableKey]; duplicate {
			closeUIReferences(references)
			return nil, nil, false, errors.New("duplicate accessibility node identity")
		}
		stableIDs[stableKey] = struct{}{}
		if len(node.States) > maxUIStatesPerNode || !validUniqueUIStates(node.States) ||
			len(node.Actions) > maxUIActionsPerNode || !validUniqueUIActions(node.Actions) {
			closeUIReferences(references)
			return nil, nil, false, errors.New("invalid accessibility state or action set")
		}
		for _, value := range []string{node.Name, node.Description, node.Value} {
			if len(value) > maxAgentUIStringBytes-rawStringBytes {
				closeUIReferences(references)
				return nil, nil, false, errors.New("accessibility text exceeds the hard aggregate limit")
			}
			rawStringBytes += len(value)
		}
		if node.Hidden || node.Offscreen {
			truncated = true
			continue
		}
		if _, allowed := policy.allowUIRole[node.Role]; !allowed {
			truncated = true
			continue
		}
		elementID := fmt.Sprintf("%s-element-%d", observationID, index+1)
		element := UIElement{ElementID: elementID, Role: node.Role}
		sensitive := node.Sensitive || node.Role == UIRolePassword
		element.Sensitive = sensitive
		if sensitive {
			element.ValueRedacted = true
		} else {
			if _, allowed := policy.allowUIProperty[UIPropertyName]; allowed {
				element.Name, truncated = consumeSanitizedUIText(node.Name, &remaining, truncated)
			}
			if _, allowed := policy.allowUIProperty[UIPropertyDescription]; allowed {
				element.Description, truncated = consumeSanitizedUIText(node.Description, &remaining, truncated)
			}
			if _, allowed := policy.allowUIProperty[UIPropertyValue]; allowed {
				element.Value, truncated = consumeSanitizedUIText(node.Value, &remaining, truncated)
			}
		}
		if _, allowed := policy.allowUIProperty[UIPropertyState]; allowed {
			element.States = append([]UIState(nil), node.States...)
		}
		if _, allowed := policy.allowUIProperty[UIPropertyActions]; allowed {
			element.Actions = append([]UIAction(nil), node.Actions...)
		}
		if _, allowed := policy.allowUIProperty[UIPropertyFocus]; allowed {
			element.Focused = node.Focused
		}
		if _, allowed := policy.allowUIProperty[UIPropertyBounds]; allowed && node.Bounds != nil {
			if !validUIBounds(node.Bounds) {
				closeUIReferences(references)
				return nil, nil, false, errors.New("invalid accessibility bounds")
			}
			bounds := *node.Bounds
			element.Bounds = &bounds
		}
		elements = append(elements, element)
		indexIDs[index] = elementID
		references[elementID] = append([]byte(nil), node.StableID...)
	}

	if _, allowed := policy.allowUIProperty[UIPropertyHierarchy]; allowed {
		byID := make(map[string]int, len(elements))
		for index := range elements {
			byID[elements[index].ElementID] = index
		}
		for backendIndex, elementID := range indexIDs {
			parent := snapshot.Nodes[backendIndex].Parent
			parentID, visible := indexIDs[parent]
			if parent < 0 || !visible {
				continue
			}
			elementIndex := byID[elementID]
			elements[elementIndex].ParentID = parentID
			parentIndex := byID[parentID]
			elements[parentIndex].ChildIDs = append(elements[parentIndex].ChildIDs, elementID)
		}
	}
	return elements, references, truncated, nil
}

func consumeSanitizedUIText(value string, remaining *int, truncated bool) (string, bool) {
	value = sanitizeUIText(value)
	if value == "" {
		return "", truncated
	}
	if *remaining <= 0 {
		return "", true
	}
	if len(value) <= *remaining {
		*remaining -= len(value)
		return value, truncated
	}
	value = truncateUTF8Bytes(value, *remaining)
	*remaining -= len(value)
	return value, true
}

func sanitizeUIText(value string) string {
	value = strings.ToValidUTF8(value, "")
	return strings.Map(func(value rune) rune {
		if unicode.IsControl(value) && value != '\n' && value != '\t' {
			return -1
		}
		return value
	}, value)
}

func truncateUTF8Bytes(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if len(value) <= limit {
		return value
	}
	value = value[:limit]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}

func validUIBounds(bounds *UIBounds) bool {
	if bounds == nil || bounds.Width <= 0 || bounds.Height <= 0 {
		return false
	}
	maximum := int(^uint(0) >> 1)
	return bounds.X <= maximum-bounds.Width && bounds.Y <= maximum-bounds.Height
}

func validUIRole(role UIRole) bool {
	switch role {
	case UIRoleApplication, UIRoleWindow, UIRoleDialog, UIRoleButton,
		UIRoleCheckbox, UIRoleRadio, UIRoleTextBox, UIRolePassword,
		UIRoleLabel, UIRoleLink, UIRoleList, UIRoleListItem, UIRoleMenu,
		UIRoleMenuItem, UIRoleTab, UIRoleTabPanel, UIRoleSlider,
		UIRoleProgress, UIRoleImage, UIRoleTable, UIRoleRow, UIRoleCell,
		UIRoleGroup, UIRoleGeneric:
		return true
	default:
		return false
	}
}

func validUIProperty(property UIProperty) bool {
	switch property {
	case UIPropertyRole, UIPropertyName, UIPropertyDescription, UIPropertyValue,
		UIPropertyState, UIPropertyBounds, UIPropertyFocus, UIPropertyActions,
		UIPropertyHierarchy:
		return true
	default:
		return false
	}
}

func validUIState(state UIState) bool {
	switch state {
	case UIStateEnabled, UIStateDisabled, UIStateChecked, UIStateSelected,
		UIStateExpanded, UIStateCollapsed, UIStateReadOnly, UIStateRequired,
		UIStateInvalid:
		return true
	default:
		return false
	}
}

func validUIAction(action UIAction) bool {
	switch action {
	case UIActionPress, UIActionFocus, UIActionSetValue, UIActionToggle,
		UIActionExpand, UIActionCollapse, UIActionIncrement, UIActionDecrement:
		return true
	default:
		return false
	}
}

func validUniqueUIStates(states []UIState) bool {
	seen := make(map[UIState]struct{}, len(states))
	for _, state := range states {
		if !validUIState(state) {
			return false
		}
		if _, duplicate := seen[state]; duplicate {
			return false
		}
		seen[state] = struct{}{}
	}
	return true
}

func validUniqueUIActions(actions []UIAction) bool {
	seen := make(map[UIAction]struct{}, len(actions))
	for _, action := range actions {
		if !validUIAction(action) {
			return false
		}
		if _, duplicate := seen[action]; duplicate {
			return false
		}
		seen[action] = struct{}{}
	}
	return true
}

func clearUIBackendSnapshot(snapshot *uiBackendSnapshot) {
	if snapshot == nil {
		return
	}
	for index := range snapshot.Nodes {
		clear(snapshot.Nodes[index].StableID)
		clear(snapshot.Nodes[index].States)
		clear(snapshot.Nodes[index].Actions)
		snapshot.Nodes[index] = uiBackendNode{}
	}
	clear(snapshot.Nodes)
	snapshot.Nodes = nil
	snapshot.Backend = ""
}

func (s *Session) storeUIObservation(id string, references map[string][]byte) {
	s.observationMu.Lock()
	s.observations[id] = observationRecord{uiElements: references}
	s.observationMu.Unlock()
}

func closeUIReferences(references map[string][]byte) {
	for id, reference := range references {
		clear(reference)
		delete(references, id)
	}
}

func uiError(code ErrorCode, message string, cause error) *ActionError {
	return newActionError(code, OperationInspectUI, message, cause)
}

func uiOperationError(err error) error {
	var actionErr *ActionError
	if errors.As(err, &actionErr) {
		return &ActionError{Code: actionErr.Code, Operation: OperationInspectUI, Message: actionErr.Message, cause: err}
	}
	code, message := classifyBackendError(err)
	return uiError(code, message, err)
}

func uiContextError(ctx context.Context) error {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return uiError(ErrorTimedOut, "UI inspection deadline exceeded", ctx.Err())
	}
	return uiError(ErrorCanceled, "UI inspection canceled", ctx.Err())
}
