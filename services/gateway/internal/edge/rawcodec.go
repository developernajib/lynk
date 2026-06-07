package edge

import (
	"fmt"
)

// rawFrame is an opaque gRPC message payload. The proxy never deserializes
// what it forwards: it has no protos, and it does not need any.
type rawFrame struct {
	payload []byte
}

// rawCodec passes frames through untouched. Installed on the proxy's server
// (ForceServerCodec) and on every upstream call (ForceCodec), it is what
// makes the proxy transparent: the default proto codec would try to parse
// the bytes and mangle the frame.
type rawCodec struct{}

// Marshal returns the frame's bytes verbatim.
func (rawCodec) Marshal(v any) ([]byte, error) {
	f, ok := v.(*rawFrame)
	if !ok {
		return nil, fmt.Errorf("edge: raw codec marshal: unexpected type %T", v)
	}
	return f.payload, nil
}

// Unmarshal stores the received bytes verbatim.
func (rawCodec) Unmarshal(data []byte, v any) error {
	f, ok := v.(*rawFrame)
	if !ok {
		return fmt.Errorf("edge: raw codec unmarshal: unexpected type %T", v)
	}
	f.payload = data
	return nil
}

// Name labels the codec in content-type negotiation. The string is
// arbitrary; both ends of the proxy use this same codec so it never has to
// match a real encoding.
func (rawCodec) Name() string { return "lynk-raw" }
