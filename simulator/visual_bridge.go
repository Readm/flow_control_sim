package simulator

// VisualBridge coordinates optional visualization publishing.
type VisualBridge[Frame any] struct {
	headless bool
	publish  func(Frame)
}

// NewVisualBridge constructs a bridge with headless flag and publish callback.
func NewVisualBridge[Frame any](headless bool, publish func(Frame)) *VisualBridge[Frame] {
	return &VisualBridge[Frame]{
		headless: headless,
		publish:  publish,
	}
}

// IsHeadless reports whether visualization output is disabled.
func (v *VisualBridge[Frame]) IsHeadless() bool {
	if v == nil {
		return true
	}
	return v.headless
}

// SetHeadless updates the headless flag.
func (v *VisualBridge[Frame]) SetHeadless(headless bool) {
	if v == nil {
		return
	}
	v.headless = headless
}

// Publish emits a frame when visualization is enabled.
func (v *VisualBridge[Frame]) Publish(frame Frame) {
	if v == nil || v.publish == nil || v.IsHeadless() {
		return
	}
	v.publish(frame)
}

// UpdatePublisher resets the publish callback.
func (v *VisualBridge[Frame]) UpdatePublisher(publish func(Frame)) {
	if v == nil {
		return
	}
	v.publish = publish
}

