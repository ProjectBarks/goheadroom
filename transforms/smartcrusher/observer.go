package smartcrusher

import "log"

// TracingObserver writes each CrushEvent at debug level. No-op in Go
// (Go doesn't have tracing levels built in; we use log for debug).
type TracingObserver struct{}

func (t TracingObserver) Name() string { return "tracing" }

func (t TracingObserver) OnEvent(event *CrushEvent) {
	log.Printf("smart_crusher.crush: strategy=%s input_bytes=%d output_bytes=%d elapsed_ns=%d was_modified=%v",
		event.Strategy, event.InputBytes, event.OutputBytes, event.ElapsedNs, event.WasModified)
}
