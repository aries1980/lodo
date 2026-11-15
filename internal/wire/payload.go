package wire

// Payload is one JSON result payload. The unexported method closes the set to
// this package, so a command cannot print an ad-hoc struct or a map on stdout.
type Payload interface {
	wirePayload()
}

func (Check) wirePayload() {}

func (Forecast) wirePayload() {}

func (Schedule) wirePayload() {}

func (Refresh) wirePayload() {}

func (Explain) wirePayload() {}

func (ExplainError) wirePayload() {}

func (Auth) wirePayload() {}
