package muninndb

// ActivateRequest describes a semantic activation query.
// See: https://github.com/scrypster/muninndb/sdk/go/muninn/types.go
type ActivateRequest struct {
	Vault      string   `json:"vault"`
	Context    []string `json:"context"`
	MaxResults int      `json:"max_results,omitempty"`
	Threshold  float64  `json:"threshold,omitempty"`
	MaxHops    int      `json:"max_hops,omitempty"`
	IncludeWhy bool     `json:"include_why,omitempty"`
	BriefMode  string   `json:"brief_mode,omitempty"`

	// Internal fields for PicoClaw compatibility
	Query string `json:"-"` // Ignored, use Context instead
	Limit int    `json:"-"` // Mapped to MaxResults
	Mode  string `json:"-"` // Ignored
}

// ActivateResponse contains activation results.
type ActivateResponse struct {
	QueryID     string           `json:"query_id"`
	TotalFound  int              `json:"total_found"`
	Activations []ActivationItem `json:"activations"`
	LatencyMs   float64          `json:"latency_ms,omitempty"`
	Brief       []BriefSentence  `json:"brief,omitempty"`
}

// ActivationItem represents a single activated memory item.
type ActivationItem struct {
	ID         string   `json:"id"`
	Concept    string   `json:"concept"`
	Content    string   `json:"content"`
	Score      float64  `json:"score"`
	Confidence float64  `json:"confidence"`
	Why        *string  `json:"why,omitempty"`
	HopPath    []string `json:"hop_path,omitempty"`
	Dormant    bool     `json:"dormant,omitempty"`
	MemoryType int      `json:"memory_type,omitempty"`
	TypeLabel  string   `json:"type_label,omitempty"`
}

// BriefSentence represents a sentence extracted by brief mode.
type BriefSentence struct {
	EngramID string  `json:"engram_id"`
	Text     string  `json:"text"`
	Score    float64 `json:"score"`
}

// WriteRequest represents a request to write an engram.
type WriteRequest struct {
	Vault         string                 `json:"vault"`
	Concept       string                 `json:"concept"`
	Content       string                 `json:"content"`
	Tags          []string               `json:"tags,omitempty"`
	Confidence    float64                `json:"confidence,omitempty"`
	Stability     float64                `json:"stability,omitempty"`
	Embedding     []float64              `json:"embedding,omitempty"`
	Associations  map[string]interface{} `json:"associations,omitempty"`
	MemoryType    *int                   `json:"memory_type,omitempty"`
	TypeLabel     string                 `json:"type_label,omitempty"`
	Summary       string                 `json:"summary,omitempty"`
	Entities      []InlineEntity         `json:"entities,omitempty"`
	Relationships []InlineRelationship   `json:"relationships,omitempty"`
}

// WriteResponse represents a response from writing an engram.
type WriteResponse struct {
	ID        string `json:"id"`
	CreatedAt int64  `json:"created_at"`
	Hint      string `json:"hint,omitempty"`
}

// InlineEntity is a caller-provided entity for inline enrichment.
type InlineEntity struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// InlineRelationship is a caller-provided relationship for inline enrichment.
type InlineRelationship struct {
	TargetID string  `json:"target_id"`
	Relation string  `json:"relation"`
	Weight   float64 `json:"weight,omitempty"`
}

// Engram represents a single memory unit in MuninnDB (for read operations).
type Engram struct {
	ID          string   `json:"id"`
	Concept     string   `json:"concept"`
	Content     string   `json:"content"`
	Confidence  float64  `json:"confidence"`
	Relevance   float64  `json:"relevance"`
	Stability   float64  `json:"stability"`
	AccessCount int      `json:"access_count"`
	Tags        []string `json:"tags"`
	State       int      `json:"state"`
	CreatedAt   int64    `json:"created_at"`
	UpdatedAt   int64    `json:"updated_at"`
	LastAccess  *int64   `json:"last_access,omitempty"`
	MemoryType  int      `json:"memory_type,omitempty"`
	TypeLabel   string   `json:"type_label,omitempty"`
}
