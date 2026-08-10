package agent

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	maxAgentCapturePixels            = 16 * 1024 * 1024
	maxAgentQueries                  = 1_000_000
	maxAgentScrollEvents             = 100
	maxAgentScrollDistance           = 100_000
	maxAgentDragDistance             = 100_000
	maxAgentDragDurationMS           = 60_000
	maxAgentChordKeys                = 5
	maxAgentActionIntervalMS         = 60_000
	maxAgentUIQueryIntervalMS        = 60_000
	maxAgentSessionTimeoutMS         = 24 * 60 * 60 * 1000
	maxAgentWindowTitleRunes         = 1024
	maxAgentUIElements               = 10_000
	maxAgentUITreeDepth              = 64
	maxAgentUIStringBytes            = 1 << 20
	maxAgentUIActionValueBytes       = 1 << 20
	maxAgentUIActionTimeoutMS        = 300_000
	maxAgentUIVerificationAttempts   = 100
	maxAgentUIVerificationIntervalMS = 60_000
	maxAgentUIVerificationTimeoutMS  = 300_000
	maxAgentViewRegions              = 1024
	maxAgentViewEncodedBytes         = 16 << 20
	maxAgentViewDimension            = 8192
	maxAgentViewIntervalMS           = 60_000
	maxAgentViewTimeoutMS            = 300_000
	maxAgentAnalysisBoxes            = 10_000
	maxAgentAnalysisTextBytes        = 1 << 20
	maxAgentAnalysisLanguages        = 16
	maxAgentAnalysisLanguageBytes    = 64
	maxAgentAnalysisIntervalMS       = 60_000
	maxAgentAnalysisTimeoutMS        = 300_000
	maxAgentVerificationAttempts     = 100
	maxAgentVerificationIntervalMS   = 60_000
	maxAgentVerificationTimeoutMS    = 300_000
	maxAgentWaitAttempts             = 100
	maxAgentWaitIntervalMS           = 60_000
	maxAgentWaitTimeoutMS            = 300_000
)

// WindowTarget is one immutable activation allow-list entry. ExpectedTitle is
// compared with the live title immediately before dispatch to reject stale or
// reused native handles.
type WindowTarget struct {
	Target        int              `json:"target"`
	Kind          WindowTargetKind `json:"kind"`
	ExpectedTitle string           `json:"expected_title"`
}

// Policy constrains every observation, query, and mutation performed by a Session.
// Empty allow lists deny access; callers must opt in explicitly.
type Policy struct {
	AllowedOperations            []Operation     `json:"allowed_operations"`
	ConfirmOperations            []Operation     `json:"confirm_operations,omitempty"`
	AllowedDisplayIDs            []int           `json:"allowed_display_ids,omitempty"`
	AllowedMouseButtons          []MouseButton   `json:"allowed_mouse_buttons,omitempty"`
	AllowedKeys                  []string        `json:"allowed_keys,omitempty"`
	AllowedModifiers             []KeyModifier   `json:"allowed_modifiers,omitempty"`
	AllowedWindows               []WindowTarget  `json:"allowed_windows,omitempty"`
	AllowedUIRoles               []UIRole        `json:"allowed_ui_roles,omitempty"`
	AllowedUIProperties          []UIProperty    `json:"allowed_ui_properties,omitempty"`
	AllowedUIActions             []UIAction      `json:"allowed_ui_actions,omitempty"`
	AllowedViewRegions           []CaptureRegion `json:"allowed_view_regions,omitempty"`
	ViewRedactionMasks           []CaptureRegion `json:"view_redaction_masks,omitempty"`
	MaxActions                   uint64          `json:"max_actions"`
	MaxTextRunes                 int             `json:"max_text_runes"`
	AllowDoubleClick             bool            `json:"allow_double_click,omitempty"`
	MaxScrollEvents              uint32          `json:"max_scroll_events,omitempty"`
	MaxScrollDistance            uint64          `json:"max_scroll_distance,omitempty"`
	MaxDragDistance              uint64          `json:"max_drag_distance,omitempty"`
	MaxDragDurationMillis        int             `json:"max_drag_duration_ms,omitempty"`
	MaxChordKeys                 uint32          `json:"max_chord_keys,omitempty"`
	MinActionIntervalMillis      int             `json:"min_action_interval_ms,omitempty"`
	MinUIQueryIntervalMillis     int             `json:"min_ui_query_interval_ms,omitempty"`
	SessionTimeoutMillis         int             `json:"session_timeout_ms,omitempty"`
	MaxObservations              uint64          `json:"max_observations,omitempty"`
	MaxCapturePixels             uint64          `json:"max_capture_pixels,omitempty"`
	MaxQueries                   uint64          `json:"max_queries,omitempty"`
	MaxUIElements                uint32          `json:"max_ui_elements,omitempty"`
	MaxUITreeDepth               uint32          `json:"max_ui_tree_depth,omitempty"`
	MaxUIStringBytes             uint32          `json:"max_ui_string_bytes,omitempty"`
	MaxUIActionValueBytes        uint32          `json:"max_ui_action_value_bytes,omitempty"`
	UIActionTimeoutMillis        int             `json:"ui_action_timeout_ms,omitempty"`
	UIVerificationAttempts       uint32          `json:"ui_verification_attempts,omitempty"`
	UIVerificationIntervalMillis int             `json:"ui_verification_interval_ms,omitempty"`
	UIVerificationTimeoutMillis  int             `json:"ui_verification_timeout_ms,omitempty"`
	AllowFullDisplayView         bool            `json:"allow_full_display_view,omitempty"`
	AllowPortalView              bool            `json:"allow_portal_view,omitempty"`
	MaxViewSourcePixels          uint64          `json:"max_view_source_pixels,omitempty"`
	MaxViewEncodedBytes          uint64          `json:"max_view_encoded_bytes,omitempty"`
	MaxViewWidth                 int             `json:"max_view_width,omitempty"`
	MaxViewHeight                int             `json:"max_view_height,omitempty"`
	MaxViews                     uint64          `json:"max_views,omitempty"`
	MaxConcurrentViews           uint32          `json:"max_concurrent_views,omitempty"`
	MinViewIntervalMillis        int             `json:"min_view_interval_ms,omitempty"`
	ViewTimeoutMillis            int             `json:"view_timeout_ms,omitempty"`
	AllowedOCRLanguages          []string        `json:"allowed_ocr_languages,omitempty"`
	AllowFullViewAnalysis        bool            `json:"allow_full_view_analysis,omitempty"`
	MaxAnalysisPixels            uint64          `json:"max_analysis_pixels,omitempty"`
	MaxOCRBoxes                  uint32          `json:"max_ocr_boxes,omitempty"`
	MaxOCRTextBytes              uint32          `json:"max_ocr_text_bytes,omitempty"`
	MaxVisualElements            uint32          `json:"max_visual_elements,omitempty"`
	MaxAnalyses                  uint64          `json:"max_analyses,omitempty"`
	MaxConcurrentAnalyses        uint32          `json:"max_concurrent_analyses,omitempty"`
	MinAnalysisIntervalMillis    int             `json:"min_analysis_interval_ms,omitempty"`
	AnalysisTimeoutMillis        int             `json:"analysis_timeout_ms,omitempty"`
	WaitAttempts                 uint32          `json:"wait_attempts,omitempty"`
	WaitIntervalMillis           int             `json:"wait_interval_ms,omitempty"`
	WaitTimeoutMillis            int             `json:"wait_timeout_ms,omitempty"`
	VerificationAttempts         uint32          `json:"verification_attempts,omitempty"`
	VerificationIntervalMillis   int             `json:"verification_interval_ms,omitempty"`
	VerificationTimeoutMillis    int             `json:"verification_timeout_ms,omitempty"`
	allowOperation               map[Operation]struct{}
	requireConfirmation          map[Operation]struct{}
	allowDisplay                 map[int]struct{}
	allowButton                  map[MouseButton]struct{}
	allowKey                     map[string]struct{}
	allowModifier                map[KeyModifier]struct{}
	allowWindow                  map[windowTargetIdentity]WindowTarget
	allowUIRole                  map[UIRole]struct{}
	allowUIProperty              map[UIProperty]struct{}
	allowUIAction                map[UIAction]struct{}
	allowOCRLanguage             map[string]struct{}
}

type windowTargetIdentity struct {
	target int
	kind   WindowTargetKind
}

// ValidatePolicy verifies every immutable session-policy bound without
// acquiring the process-exclusive session owner or touching a desktop
// backend. Callers that must complete an explicit local consent step before
// NewSession can use it to fail closed before presenting that prompt.
func ValidatePolicy(policy Policy) error {
	_, err := preparePolicy(policy)
	return err
}

func preparePolicy(input Policy) (Policy, error) {
	if input.MaxTextRunes < 0 {
		return Policy{}, fmt.Errorf("agent: max text runes must be non-negative")
	}
	if input.VerificationIntervalMillis < 0 {
		return Policy{}, fmt.Errorf("agent: verification interval must be non-negative")
	}
	if input.MaxQueries > maxAgentQueries {
		return Policy{}, fmt.Errorf("agent: max queries exceeds hard limit %d", maxAgentQueries)
	}
	if input.MaxUIElements > maxAgentUIElements {
		return Policy{}, fmt.Errorf("agent: max UI elements exceeds hard limit %d", maxAgentUIElements)
	}
	if input.MaxUITreeDepth > maxAgentUITreeDepth {
		return Policy{}, fmt.Errorf("agent: max UI tree depth exceeds hard limit %d", maxAgentUITreeDepth)
	}
	if input.MaxUIStringBytes > maxAgentUIStringBytes {
		return Policy{}, fmt.Errorf("agent: max UI string bytes exceeds hard limit %d", maxAgentUIStringBytes)
	}
	if input.MaxUIActionValueBytes > maxAgentUIActionValueBytes {
		return Policy{}, fmt.Errorf("agent: max UI action value bytes exceeds hard limit %d", maxAgentUIActionValueBytes)
	}
	if input.UIActionTimeoutMillis < 0 || input.UIActionTimeoutMillis > maxAgentUIActionTimeoutMS {
		return Policy{}, fmt.Errorf("agent: semantic action timeout must be between 0 and %dms", maxAgentUIActionTimeoutMS)
	}
	if input.UIVerificationAttempts > maxAgentUIVerificationAttempts {
		return Policy{}, fmt.Errorf("agent: UI verification attempts exceeds hard limit %d", maxAgentUIVerificationAttempts)
	}
	if input.UIVerificationIntervalMillis < 0 || input.UIVerificationIntervalMillis > maxAgentUIVerificationIntervalMS {
		return Policy{}, fmt.Errorf("agent: UI verification interval must be between 0 and %dms", maxAgentUIVerificationIntervalMS)
	}
	if input.UIVerificationTimeoutMillis < 0 || input.UIVerificationTimeoutMillis > maxAgentUIVerificationTimeoutMS {
		return Policy{}, fmt.Errorf("agent: UI verification timeout must be between 0 and %dms", maxAgentUIVerificationTimeoutMS)
	}
	if (input.UIVerificationAttempts == 0) != (input.UIVerificationTimeoutMillis == 0) {
		return Policy{}, fmt.Errorf("agent: UI verification attempts and timeout must both be zero or both be positive")
	}
	if len(input.AllowedViewRegions) > maxAgentViewRegions || len(input.ViewRedactionMasks) > maxAgentViewRegions {
		return Policy{}, fmt.Errorf("agent: view region or redaction mask count exceeds hard limit %d", maxAgentViewRegions)
	}
	if input.MaxViewSourcePixels > maxAgentCapturePixels {
		return Policy{}, fmt.Errorf("agent: max view source pixels exceeds hard limit %d", maxAgentCapturePixels)
	}
	if input.MaxViewEncodedBytes > maxAgentViewEncodedBytes {
		return Policy{}, fmt.Errorf("agent: max view encoded bytes exceeds hard limit %d", maxAgentViewEncodedBytes)
	}
	if input.MaxViewWidth < 0 || input.MaxViewWidth > maxAgentViewDimension ||
		input.MaxViewHeight < 0 || input.MaxViewHeight > maxAgentViewDimension {
		return Policy{}, fmt.Errorf("agent: max view dimensions must be between 0 and %d", maxAgentViewDimension)
	}
	if input.MaxConcurrentViews > 1 {
		return Policy{}, fmt.Errorf("agent: max concurrent views exceeds the process-safe limit 1")
	}
	if input.MaxViews > maxAgentQueries {
		return Policy{}, fmt.Errorf("agent: max views exceeds hard limit %d", maxAgentQueries)
	}
	if input.MinViewIntervalMillis < 0 || input.MinViewIntervalMillis > maxAgentViewIntervalMS {
		return Policy{}, fmt.Errorf("agent: minimum view interval must be between 0 and %dms", maxAgentViewIntervalMS)
	}
	if input.ViewTimeoutMillis < 0 || input.ViewTimeoutMillis > maxAgentViewTimeoutMS {
		return Policy{}, fmt.Errorf("agent: view timeout must be between 0 and %dms", maxAgentViewTimeoutMS)
	}
	if input.MaxAnalysisPixels > maxAgentCapturePixels {
		return Policy{}, fmt.Errorf("agent: max analysis pixels exceeds hard limit %d", maxAgentCapturePixels)
	}
	if input.MaxOCRBoxes > maxAgentAnalysisBoxes || input.MaxVisualElements > maxAgentAnalysisBoxes {
		return Policy{}, fmt.Errorf("agent: analysis result count exceeds hard limit %d", maxAgentAnalysisBoxes)
	}
	if input.MaxOCRTextBytes > maxAgentAnalysisTextBytes {
		return Policy{}, fmt.Errorf("agent: max OCR text bytes exceeds hard limit %d", maxAgentAnalysisTextBytes)
	}
	if len(input.AllowedOCRLanguages) > maxAgentAnalysisLanguages {
		return Policy{}, fmt.Errorf("agent: OCR language count exceeds hard limit %d", maxAgentAnalysisLanguages)
	}
	if input.MaxConcurrentAnalyses > 1 {
		return Policy{}, fmt.Errorf("agent: max concurrent analyses exceeds the process-safe limit 1")
	}
	if input.MaxAnalyses > maxAgentQueries {
		return Policy{}, fmt.Errorf("agent: max analyses exceeds hard limit %d", maxAgentQueries)
	}
	if input.MinAnalysisIntervalMillis < 0 || input.MinAnalysisIntervalMillis > maxAgentAnalysisIntervalMS {
		return Policy{}, fmt.Errorf("agent: minimum analysis interval must be between 0 and %dms", maxAgentAnalysisIntervalMS)
	}
	if input.AnalysisTimeoutMillis < 0 || input.AnalysisTimeoutMillis > maxAgentAnalysisTimeoutMS {
		return Policy{}, fmt.Errorf("agent: analysis timeout must be between 0 and %dms", maxAgentAnalysisTimeoutMS)
	}
	if input.MaxScrollEvents > maxAgentScrollEvents {
		return Policy{}, fmt.Errorf("agent: max scroll events exceeds hard limit %d", maxAgentScrollEvents)
	}
	if input.MaxScrollDistance > maxAgentScrollDistance {
		return Policy{}, fmt.Errorf("agent: max scroll distance exceeds hard limit %d", maxAgentScrollDistance)
	}
	if input.MaxDragDistance > maxAgentDragDistance {
		return Policy{}, fmt.Errorf("agent: max drag distance exceeds hard limit %d", maxAgentDragDistance)
	}
	if input.MaxDragDurationMillis < 0 || input.MaxDragDurationMillis > maxAgentDragDurationMS {
		return Policy{}, fmt.Errorf("agent: max drag duration must be between 0 and %dms", maxAgentDragDurationMS)
	}
	if input.MaxChordKeys > maxAgentChordKeys {
		return Policy{}, fmt.Errorf("agent: max chord keys exceeds hard limit %d", maxAgentChordKeys)
	}
	if input.MinActionIntervalMillis < 0 || input.MinActionIntervalMillis > maxAgentActionIntervalMS {
		return Policy{}, fmt.Errorf("agent: minimum action interval must be between 0 and %dms", maxAgentActionIntervalMS)
	}
	if input.MinUIQueryIntervalMillis < 0 || input.MinUIQueryIntervalMillis > maxAgentUIQueryIntervalMS {
		return Policy{}, fmt.Errorf("agent: minimum UI query interval must be between 0 and %dms", maxAgentUIQueryIntervalMS)
	}
	if input.SessionTimeoutMillis < 0 || input.SessionTimeoutMillis > maxAgentSessionTimeoutMS {
		return Policy{}, fmt.Errorf("agent: session timeout must be between 0 and %dms", maxAgentSessionTimeoutMS)
	}
	if input.WaitAttempts > maxAgentWaitAttempts {
		return Policy{}, fmt.Errorf("agent: wait attempts exceeds hard limit %d", maxAgentWaitAttempts)
	}
	if input.WaitIntervalMillis < 0 || input.WaitIntervalMillis > maxAgentWaitIntervalMS {
		return Policy{}, fmt.Errorf("agent: wait interval must be between 0 and %dms", maxAgentWaitIntervalMS)
	}
	if input.WaitTimeoutMillis < 0 || input.WaitTimeoutMillis > maxAgentWaitTimeoutMS {
		return Policy{}, fmt.Errorf("agent: wait timeout must be between 0 and %dms", maxAgentWaitTimeoutMS)
	}
	if (input.WaitAttempts == 0) != (input.WaitTimeoutMillis == 0) {
		return Policy{}, fmt.Errorf("agent: wait attempts and timeout must both be zero or both be positive")
	}
	if input.MaxCapturePixels > maxAgentCapturePixels {
		return Policy{}, fmt.Errorf("agent: max capture pixels exceeds hard limit %d", maxAgentCapturePixels)
	}
	if input.VerificationAttempts > maxAgentVerificationAttempts {
		return Policy{}, fmt.Errorf("agent: verification attempts exceeds hard limit %d", maxAgentVerificationAttempts)
	}
	if input.VerificationIntervalMillis > maxAgentVerificationIntervalMS {
		return Policy{}, fmt.Errorf("agent: verification interval exceeds hard limit %dms", maxAgentVerificationIntervalMS)
	}
	if input.VerificationTimeoutMillis < 0 || input.VerificationTimeoutMillis > maxAgentVerificationTimeoutMS {
		return Policy{}, fmt.Errorf("agent: verification timeout must be between 0 and %dms", maxAgentVerificationTimeoutMS)
	}
	if (input.VerificationAttempts == 0) != (input.VerificationTimeoutMillis == 0) {
		return Policy{}, fmt.Errorf("agent: verification attempts and timeout must both be zero or both be positive")
	}
	prepared := Policy{
		AllowedOperations:   append([]Operation(nil), input.AllowedOperations...),
		ConfirmOperations:   append([]Operation(nil), input.ConfirmOperations...),
		AllowedDisplayIDs:   append([]int(nil), input.AllowedDisplayIDs...),
		AllowedMouseButtons: append([]MouseButton(nil), input.AllowedMouseButtons...),
		AllowedKeys:         append([]string(nil), input.AllowedKeys...),
		AllowedModifiers:    append([]KeyModifier(nil), input.AllowedModifiers...),
		AllowedWindows:      append([]WindowTarget(nil), input.AllowedWindows...),
		AllowedUIRoles:      append([]UIRole(nil), input.AllowedUIRoles...),
		AllowedUIProperties: append([]UIProperty(nil), input.AllowedUIProperties...),
		AllowedUIActions:    append([]UIAction(nil), input.AllowedUIActions...),
		AllowedViewRegions:  append([]CaptureRegion(nil), input.AllowedViewRegions...),
		ViewRedactionMasks:  append([]CaptureRegion(nil), input.ViewRedactionMasks...),
		MaxActions:          input.MaxActions, MaxTextRunes: input.MaxTextRunes,
		AllowDoubleClick:             input.AllowDoubleClick,
		MaxScrollEvents:              input.MaxScrollEvents,
		MaxScrollDistance:            input.MaxScrollDistance,
		MaxDragDistance:              input.MaxDragDistance,
		MaxDragDurationMillis:        input.MaxDragDurationMillis,
		MaxChordKeys:                 input.MaxChordKeys,
		MinActionIntervalMillis:      input.MinActionIntervalMillis,
		MinUIQueryIntervalMillis:     input.MinUIQueryIntervalMillis,
		SessionTimeoutMillis:         input.SessionTimeoutMillis,
		MaxObservations:              input.MaxObservations,
		MaxCapturePixels:             input.MaxCapturePixels,
		MaxQueries:                   input.MaxQueries,
		MaxUIElements:                input.MaxUIElements,
		MaxUITreeDepth:               input.MaxUITreeDepth,
		MaxUIStringBytes:             input.MaxUIStringBytes,
		MaxUIActionValueBytes:        input.MaxUIActionValueBytes,
		UIActionTimeoutMillis:        input.UIActionTimeoutMillis,
		UIVerificationAttempts:       input.UIVerificationAttempts,
		UIVerificationIntervalMillis: input.UIVerificationIntervalMillis,
		UIVerificationTimeoutMillis:  input.UIVerificationTimeoutMillis,
		AllowFullDisplayView:         input.AllowFullDisplayView,
		AllowPortalView:              input.AllowPortalView,
		MaxViewSourcePixels:          input.MaxViewSourcePixels,
		MaxViewEncodedBytes:          input.MaxViewEncodedBytes,
		MaxViewWidth:                 input.MaxViewWidth,
		MaxViewHeight:                input.MaxViewHeight,
		MaxViews:                     input.MaxViews,
		MaxConcurrentViews:           input.MaxConcurrentViews,
		MinViewIntervalMillis:        input.MinViewIntervalMillis,
		ViewTimeoutMillis:            input.ViewTimeoutMillis,
		AllowedOCRLanguages:          append([]string(nil), input.AllowedOCRLanguages...),
		AllowFullViewAnalysis:        input.AllowFullViewAnalysis,
		MaxAnalysisPixels:            input.MaxAnalysisPixels,
		MaxOCRBoxes:                  input.MaxOCRBoxes,
		MaxOCRTextBytes:              input.MaxOCRTextBytes,
		MaxVisualElements:            input.MaxVisualElements,
		MaxAnalyses:                  input.MaxAnalyses,
		MaxConcurrentAnalyses:        input.MaxConcurrentAnalyses,
		MinAnalysisIntervalMillis:    input.MinAnalysisIntervalMillis,
		AnalysisTimeoutMillis:        input.AnalysisTimeoutMillis,
		WaitAttempts:                 input.WaitAttempts,
		WaitIntervalMillis:           input.WaitIntervalMillis,
		WaitTimeoutMillis:            input.WaitTimeoutMillis,
		VerificationAttempts:         input.VerificationAttempts,
		VerificationIntervalMillis:   input.VerificationIntervalMillis,
		VerificationTimeoutMillis:    input.VerificationTimeoutMillis,
		allowOperation:               make(map[Operation]struct{}),
		requireConfirmation:          make(map[Operation]struct{}),
		allowDisplay:                 make(map[int]struct{}),
		allowButton:                  make(map[MouseButton]struct{}),
		allowKey:                     make(map[string]struct{}),
		allowModifier:                make(map[KeyModifier]struct{}),
		allowWindow:                  make(map[windowTargetIdentity]WindowTarget),
		allowUIRole:                  make(map[UIRole]struct{}),
		allowUIProperty:              make(map[UIProperty]struct{}),
		allowUIAction:                make(map[UIAction]struct{}),
		allowOCRLanguage:             make(map[string]struct{}),
	}
	for _, operation := range prepared.AllowedOperations {
		if !knownOperation(operation) {
			return Policy{}, fmt.Errorf("agent: unknown allowed operation %q", operation)
		}
		prepared.allowOperation[operation] = struct{}{}
	}
	if prepared.MaxActions == 0 && allowsMutation(prepared.allowOperation) {
		return Policy{}, fmt.Errorf("agent: max actions must be positive when a mutation is allowed")
	}
	if _, allowed := prepared.allowOperation[OperationObserve]; allowed && prepared.MaxObservations == 0 {
		return Policy{}, fmt.Errorf("agent: max observations must be positive when desktop.observe is allowed")
	}
	for _, operation := range prepared.ConfirmOperations {
		if _, allowed := prepared.allowOperation[operation]; !allowed {
			return Policy{}, fmt.Errorf("agent: confirmation operation %q is not allowed", operation)
		}
		prepared.requireConfirmation[operation] = struct{}{}
	}
	for _, displayID := range prepared.AllowedDisplayIDs {
		if displayID < 0 {
			return Policy{}, fmt.Errorf("agent: allowed display IDs must be non-negative")
		}
		prepared.allowDisplay[displayID] = struct{}{}
	}
	for _, button := range prepared.AllowedMouseButtons {
		if !validMouseButton(button) {
			return Policy{}, fmt.Errorf("agent: unsupported allowed mouse button %q", button)
		}
		if _, exists := prepared.allowButton[button]; exists {
			return Policy{}, fmt.Errorf("agent: duplicate allowed mouse button %q", button)
		}
		prepared.allowButton[button] = struct{}{}
	}
	for _, key := range prepared.AllowedKeys {
		if !validChordKey(key) {
			return Policy{}, fmt.Errorf("agent: unsupported or non-canonical allowed chord key %q", key)
		}
		if _, exists := prepared.allowKey[key]; exists {
			return Policy{}, fmt.Errorf("agent: duplicate allowed chord key %q", key)
		}
		prepared.allowKey[key] = struct{}{}
	}
	for _, modifier := range prepared.AllowedModifiers {
		if !validKeyModifier(modifier) {
			return Policy{}, fmt.Errorf("agent: unsupported allowed key modifier %q", modifier)
		}
		if _, exists := prepared.allowModifier[modifier]; exists {
			return Policy{}, fmt.Errorf("agent: duplicate allowed key modifier %q", modifier)
		}
		prepared.allowModifier[modifier] = struct{}{}
	}
	for _, window := range prepared.AllowedWindows {
		if window.Target <= 0 || !validWindowTargetKind(window.Kind) {
			return Policy{}, fmt.Errorf("agent: allowed window requires a positive target and valid kind")
		}
		if window.ExpectedTitle == "" || !utf8.ValidString(window.ExpectedTitle) ||
			utf8.RuneCountInString(window.ExpectedTitle) > maxAgentWindowTitleRunes {
			return Policy{}, fmt.Errorf("agent: allowed window requires a bounded non-empty valid UTF-8 expected title")
		}
		identity := windowTargetIdentity{target: window.Target, kind: window.Kind}
		if _, exists := prepared.allowWindow[identity]; exists {
			return Policy{}, fmt.Errorf("agent: duplicate allowed window target %d (%s)", window.Target, window.Kind)
		}
		prepared.allowWindow[identity] = window
	}
	for _, role := range prepared.AllowedUIRoles {
		if !validUIRole(role) {
			return Policy{}, fmt.Errorf("agent: unsupported allowed UI role %q", role)
		}
		if _, exists := prepared.allowUIRole[role]; exists {
			return Policy{}, fmt.Errorf("agent: duplicate allowed UI role %q", role)
		}
		prepared.allowUIRole[role] = struct{}{}
	}
	for _, property := range prepared.AllowedUIProperties {
		if !validUIProperty(property) {
			return Policy{}, fmt.Errorf("agent: unsupported allowed UI property %q", property)
		}
		if _, exists := prepared.allowUIProperty[property]; exists {
			return Policy{}, fmt.Errorf("agent: duplicate allowed UI property %q", property)
		}
		prepared.allowUIProperty[property] = struct{}{}
	}
	for _, action := range prepared.AllowedUIActions {
		if !validUIAction(action) {
			return Policy{}, fmt.Errorf("agent: unsupported allowed UI action %q", action)
		}
		if _, exists := prepared.allowUIAction[action]; exists {
			return Policy{}, fmt.Errorf("agent: duplicate allowed UI action %q", action)
		}
		prepared.allowUIAction[action] = struct{}{}
	}
	for _, language := range prepared.AllowedOCRLanguages {
		if !validOCRLanguage(language) {
			return Policy{}, fmt.Errorf("agent: invalid allowed OCR language %q", language)
		}
		if _, exists := prepared.allowOCRLanguage[language]; exists {
			return Policy{}, fmt.Errorf("agent: duplicate allowed OCR language %q", language)
		}
		prepared.allowOCRLanguage[language] = struct{}{}
	}
	for _, region := range prepared.AllowedViewRegions {
		if err := validateCaptureRegion(region, maxAgentCapturePixels); err != nil {
			return Policy{}, fmt.Errorf("agent: invalid allowed view region: %w", err)
		}
		if _, allowed := prepared.allowDisplay[region.DisplayID]; !allowed {
			return Policy{}, fmt.Errorf("agent: allowed view region requires an allowed display ID")
		}
	}
	for _, mask := range prepared.ViewRedactionMasks {
		if err := validateCaptureRegion(mask, maxAgentCapturePixels); err != nil {
			return Policy{}, fmt.Errorf("agent: invalid view redaction mask: %w", err)
		}
		if _, allowed := prepared.allowDisplay[mask.DisplayID]; !allowed {
			return Policy{}, fmt.Errorf("agent: view redaction mask requires an allowed display ID")
		}
	}
	if _, allowed := prepared.allowOperation[OperationClick]; allowed && len(prepared.allowButton) == 0 {
		return Policy{}, fmt.Errorf("agent: pointer.click requires allowed mouse buttons")
	}
	if _, allowed := prepared.allowOperation[OperationScroll]; allowed {
		if len(prepared.allowDisplay) == 0 || prepared.MaxScrollEvents == 0 ||
			prepared.MaxScrollDistance == 0 {
			return Policy{}, fmt.Errorf("agent: pointer.scroll requires allowed displays and bounded events and distance")
		}
	}
	if _, allowed := prepared.allowOperation[OperationDrag]; allowed {
		if len(prepared.allowDisplay) == 0 || len(prepared.allowButton) == 0 ||
			prepared.MaxDragDistance == 0 || prepared.MaxDragDurationMillis == 0 {
			return Policy{}, fmt.Errorf("agent: pointer.drag requires allowed displays and buttons plus bounded distance and duration")
		}
	}
	if _, allowed := prepared.allowOperation[OperationKeyChord]; allowed {
		hasProcessTarget := false
		for identity := range prepared.allowWindow {
			if identity.kind == WindowTargetProcess {
				hasProcessTarget = true
				break
			}
		}
		if len(prepared.allowKey) == 0 || prepared.MaxChordKeys == 0 || !hasProcessTarget {
			return Policy{}, fmt.Errorf("agent: keyboard.chord requires allowed keys, process windows, and a bounded chord length")
		}
	}
	if _, allowed := prepared.allowOperation[OperationActivate]; allowed && len(prepared.allowWindow) == 0 {
		return Policy{}, fmt.Errorf("agent: window.activate requires allowed window identities")
	}
	if _, allowed := prepared.allowOperation[OperationInspectUI]; allowed {
		if len(prepared.allowWindow) == 0 || len(prepared.allowUIRole) == 0 ||
			len(prepared.allowUIProperty) == 0 || prepared.MaxQueries == 0 ||
			prepared.MaxObservations == 0 || prepared.MaxUIElements == 0 ||
			prepared.MaxUITreeDepth == 0 || prepared.MaxUIStringBytes == 0 ||
			prepared.MinUIQueryIntervalMillis == 0 || prepared.SessionTimeoutMillis == 0 {
			return Policy{}, fmt.Errorf("agent: desktop.inspect-ui requires allowed windows, roles, properties, and bounded query, observation, node, depth, string, rate, and lifetime limits")
		}
		if _, allowed := prepared.allowUIProperty[UIPropertyRole]; !allowed {
			return Policy{}, fmt.Errorf("agent: desktop.inspect-ui requires the role property")
		}
	}
	if _, allowed := prepared.allowOperation[OperationElementAct]; allowed {
		if _, inspectAllowed := prepared.allowOperation[OperationInspectUI]; !inspectAllowed {
			return Policy{}, fmt.Errorf("agent: desktop.element-act requires desktop.inspect-ui")
		}
		if len(prepared.allowUIAction) == 0 || prepared.MaxActions == 0 || prepared.UIActionTimeoutMillis == 0 ||
			prepared.MinActionIntervalMillis == 0 || prepared.SessionTimeoutMillis == 0 {
			return Policy{}, fmt.Errorf("agent: desktop.element-act requires allowed semantic actions and bounded action count, rate, action duration, and lifetime")
		}
		if _, allowed := prepared.allowUIAction[UIActionSetValue]; allowed && prepared.MaxUIActionValueBytes == 0 {
			return Policy{}, fmt.Errorf("agent: semantic set-value requires a bounded value byte limit")
		}
		for _, property := range []UIProperty{UIPropertyName, UIPropertyState, UIPropertyBounds, UIPropertyActions} {
			if _, allowed := prepared.allowUIProperty[property]; !allowed {
				return Policy{}, fmt.Errorf("agent: desktop.element-act requires the %s property", property)
			}
		}
		if prepared.UIVerificationAttempts > 0 {
			minimumReads := minimumUIElementWorkflowReads(prepared.UIVerificationAttempts)
			if prepared.MaxQueries < minimumReads || prepared.MaxObservations < minimumReads {
				return Policy{}, fmt.Errorf("agent: semantic verification requires at least %d query and observation slots", minimumReads)
			}
		}
	} else if prepared.UIVerificationAttempts > 0 {
		return Policy{}, fmt.Errorf("agent: UI verification requires desktop.element-act")
	}
	if _, allowed := prepared.allowOperation[OperationView]; allowed {
		if len(prepared.allowDisplay) == 0 ||
			(len(prepared.AllowedViewRegions) == 0 && !prepared.AllowFullDisplayView) ||
			prepared.MaxViewSourcePixels == 0 || prepared.MaxViewEncodedBytes == 0 ||
			prepared.MaxViewWidth == 0 || prepared.MaxViewHeight == 0 ||
			prepared.MaxViews == 0 || prepared.MaxObservations == 0 ||
			prepared.MaxConcurrentViews != 1 || prepared.MinViewIntervalMillis == 0 ||
			prepared.ViewTimeoutMillis == 0 || prepared.SessionTimeoutMillis == 0 {
			return Policy{}, fmt.Errorf("agent: desktop.view requires allowed displays and regions plus bounded source pixels, encoded bytes, dimensions, count, concurrency, rate, view duration, observation count, and session lifetime")
		}
	}
	_, ocrAllowed := prepared.allowOperation[OperationOCR]
	_, detectionAllowed := prepared.allowOperation[OperationDetectElements]
	if ocrAllowed || detectionAllowed {
		if _, viewAllowed := prepared.allowOperation[OperationView]; !viewAllowed {
			return Policy{}, fmt.Errorf("agent: image analysis requires desktop.view")
		}
		if prepared.MaxAnalysisPixels == 0 || prepared.MaxAnalyses == 0 ||
			prepared.MaxConcurrentAnalyses != 1 || prepared.MinAnalysisIntervalMillis == 0 ||
			prepared.AnalysisTimeoutMillis == 0 || prepared.SessionTimeoutMillis == 0 {
			return Policy{}, fmt.Errorf("agent: image analysis requires bounded pixels, count, concurrency, rate, duration, and session lifetime")
		}
	}
	if ocrAllowed && (len(prepared.allowOCRLanguage) == 0 || prepared.MaxOCRBoxes == 0 || prepared.MaxOCRTextBytes == 0) {
		return Policy{}, fmt.Errorf("agent: desktop.ocr requires allowed languages and bounded boxes and text bytes")
	}
	if detectionAllowed && prepared.MaxVisualElements == 0 {
		return Policy{}, fmt.Errorf("agent: desktop.detect-elements requires a bounded element count")
	}
	if allowsExtendedMutation(prepared.allowOperation) {
		if prepared.MinActionIntervalMillis == 0 || prepared.SessionTimeoutMillis == 0 {
			return Policy{}, fmt.Errorf("agent: extended mutations require bounded action interval and session timeout")
		}
	}
	_, findAllowed := prepared.allowOperation[OperationFindColor]
	_, waitAllowed := prepared.allowOperation[OperationWaitColor]
	if (findAllowed || waitAllowed) && prepared.MaxQueries == 0 {
		return Policy{}, fmt.Errorf("agent: max queries must be positive when a visual query is allowed")
	}
	if findAllowed {
		if _, observeAllowed := prepared.allowOperation[OperationObserve]; !observeAllowed {
			return Policy{}, fmt.Errorf("agent: desktop.find-color requires desktop.observe")
		}
		if prepared.MaxCapturePixels == 0 || len(prepared.allowDisplay) == 0 {
			return Policy{}, fmt.Errorf("agent: desktop.find-color requires allowed bounded captures")
		}
	}
	if waitAllowed {
		if prepared.WaitAttempts == 0 || prepared.WaitTimeoutMillis == 0 {
			return Policy{}, fmt.Errorf("agent: desktop.wait-color requires bounded wait attempts and timeout")
		}
		if prepared.MaxCapturePixels == 0 || len(prepared.allowDisplay) == 0 {
			return Policy{}, fmt.Errorf("agent: desktop.wait-color requires allowed bounded captures")
		}
		if prepared.MaxObservations < uint64(prepared.WaitAttempts) {
			return Policy{}, fmt.Errorf("agent: desktop.wait-color requires at least %d observations", prepared.WaitAttempts)
		}
	}
	if prepared.VerificationAttempts > 0 {
		_, observeAllowed := prepared.allowOperation[OperationObserve]
		_, viewAllowed := prepared.allowOperation[OperationView]
		if ((!observeAllowed || prepared.MaxCapturePixels == 0) &&
			(!viewAllowed || prepared.MaxViewSourcePixels == 0)) || len(prepared.allowDisplay) == 0 {
			return Policy{}, fmt.Errorf("agent: verification requires allowed bounded capture observations")
		}
		minimumObservations := uint64(prepared.VerificationAttempts) + 2
		if prepared.MaxObservations < minimumObservations {
			return Policy{}, fmt.Errorf("agent: verification requires at least %d observations", minimumObservations)
		}
	}
	return prepared, nil
}

func allowsMutation(operations map[Operation]struct{}) bool {
	for _, operation := range []Operation{
		OperationMove, OperationClick, OperationScroll, OperationDrag,
		OperationTypeText, OperationKeyChord, OperationActivate,
		OperationElementAct,
	} {
		if _, allowed := operations[operation]; allowed {
			return true
		}
	}
	return false
}

func allowsExtendedMutation(operations map[Operation]struct{}) bool {
	for _, operation := range []Operation{
		OperationScroll, OperationDrag, OperationKeyChord, OperationActivate,
		OperationElementAct,
	} {
		if _, allowed := operations[operation]; allowed {
			return true
		}
	}
	return false
}

func knownOperation(operation Operation) bool {
	switch operation {
	case OperationMove, OperationClick, OperationScroll, OperationDrag,
		OperationTypeText, OperationKeyChord, OperationActivate,
		OperationObserve, OperationView, OperationOCR, OperationDetectElements,
		OperationInspectUI, OperationFindColor, OperationWaitColor:
		return true
	case OperationElementAct:
		return true
	default:
		return false
	}
}

func validOCRLanguage(language string) bool {
	if language == "" || len(language) > maxAgentAnalysisLanguageBytes {
		return false
	}
	for _, value := range language {
		if value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' ||
			value >= '0' && value <= '9' || value == '_' || value == '-' {
			continue
		}
		return false
	}
	return true
}

func validMouseButton(button MouseButton) bool {
	switch button {
	case MouseButtonLeft, MouseButtonMiddle, MouseButtonRight:
		return true
	default:
		return false
	}
}

func validKeyModifier(modifier KeyModifier) bool {
	switch modifier {
	case KeyModifierAlt, KeyModifierControl, KeyModifierMeta, KeyModifierShift:
		return true
	default:
		return false
	}
}

func validChordKey(key string) bool {
	if len(key) == 1 {
		value := key[0]
		return value >= 'a' && value <= 'z' || value >= '0' && value <= '9' ||
			strings.ContainsRune("-=[]\\;',./`", rune(value))
	}
	switch key {
	case "backspace", "delete", "enter", "tab", "escape",
		"up", "down", "right", "left", "home", "end", "pageup",
		"pagedown", "space",
		"f1", "f2", "f3", "f4", "f5", "f6",
		"f7", "f8", "f9", "f10", "f11", "f12":
		return true
	default:
		return false
	}
}

func mandatoryConfirmation(operation Operation) bool {
	switch operation {
	case OperationDrag, OperationKeyChord, OperationActivate:
		return true
	default:
		return false
	}
}

func validWindowTargetKind(kind WindowTargetKind) bool {
	switch kind {
	case WindowTargetProcess, WindowTargetHandle:
		return true
	default:
		return false
	}
}
