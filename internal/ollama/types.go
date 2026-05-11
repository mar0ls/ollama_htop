package ollama

import "time"

// Details holds model parameter metadata from /api/ps.
type Details struct {
	Family    string `json:"family"`
	ParamSize string `json:"parameter_size"`
	Quant     string `json:"quantization_level"`
}

// LoadedModel is one running model from /api/ps.
type LoadedModel struct {
	Name      string    `json:"name"`
	Size      int64     `json:"size"`
	SizeVRAM  int64     `json:"size_vram"`
	Digest    string    `json:"digest"`
	Details   Details   `json:"details"`
	ExpiresAt time.Time `json:"expires_at"`
}

// State is the result of one poll round.
type State struct {
	Models    []LoadedModel
	Connected bool
	Version   string
	Err       error
	At        time.Time
}
