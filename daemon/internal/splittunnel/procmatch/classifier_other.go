//go:build !windows && !darwin && !linux

package procmatch

func NewClassifier(opts Options) (Classifier, error) {
	return nil, ErrUnsupported
}
