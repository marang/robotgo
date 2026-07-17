//go:build !cgo

package robotgo

func currentTreatAsHandle() bool { return GetRuntimeConfig().TreatAsHandle }
