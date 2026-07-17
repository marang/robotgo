//go:build !cgo && !darwin

package robotgo

func platformDarwinScale(...int) float64 { return 1 }
