package order

import "math"

// CalculateFare computes the trip fare from distance, duration, and config.
// Returns baseFare, finalFare.
func CalculateFare(fc *FareConfig, distanceMeters int, durationSeconds int, surgeMultiplier float64) (base, final float64) {
	distKm := float64(distanceMeters) / 1000.0
	durationMin := float64(durationSeconds) / 60.0

	base = fc.BaseFare + (distKm * fc.PerKmRate) + (durationMin * fc.PerMinRate)
	base = math.Round(base/100) * 100 // round to nearest 100 IDR

	final = base * surgeMultiplier
	final = math.Round(final/100) * 100

	if final < fc.MinFare {
		final = fc.MinFare
		base = fc.MinFare
	}
	return base, final
}
