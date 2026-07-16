//go:build cgo

package robotgo

func currentTreatAsHandle() bool { return GetRuntimeConfig().TreatAsHandle }
func currentScale() bool         { return GetRuntimeConfig().Scale }
