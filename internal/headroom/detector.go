package headroom

// DefaultDetector performs heuristic classification.
type DefaultDetector struct{}

// Detect implements the Detector interface.
func (d *DefaultDetector) Detect(in Input) (Kind, float64) {
	return Detect(in)
}
