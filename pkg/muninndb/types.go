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

// LinkRequest creates or updates a semantic relationship between two engrams.
type LinkRequest struct {
	Vault    string  `json:"vault"`
	SourceID string  `json:"source_id"`
	TargetID string  `json:"target_id"`
	Relation string  `json:"relation"`
	Weight   float64 `json:"weight,omitempty"`
}

// LinkResponse represents the result of linking two engrams.
type LinkResponse struct {
	OK      bool   `json:"ok,omitempty"`
	EdgeID  string `json:"edge_id,omitempty"`
	Message string `json:"message,omitempty"`
}

// TraverseRequest explores the memory graph starting from an engram or query.
type TraverseRequest struct {
	Vault      string   `json:"vault"`
	StartID    string   `json:"start_id,omitempty"`
	Query      string   `json:"query,omitempty"`
	Directions []string `json:"directions,omitempty"`
	Relations  []string `json:"relations,omitempty"`
	MaxDepth   int      `json:"max_depth,omitempty"`
	Limit      int      `json:"limit,omitempty"`
}

// TraverseNode represents an engram returned by graph traversal.
type TraverseNode struct {
	ID      string   `json:"id"`
	Concept string   `json:"concept,omitempty"`
	Content string   `json:"content,omitempty"`
	Path    []string `json:"path,omitempty"`
	Depth   int      `json:"depth,omitempty"`
	Score   float64  `json:"score,omitempty"`
}

// TraverseResponse contains graph traversal results.
type TraverseResponse struct {
	Nodes   []TraverseNode `json:"nodes,omitempty"`
	Summary string         `json:"summary,omitempty"`
}

// ExplainRequest requests explanation for an engram or recall query.
type ExplainRequest struct {
	Vault    string `json:"vault"`
	EngramID string `json:"engram_id,omitempty"`
	Query    string `json:"query,omitempty"`
}

// ExplainFactor describes one explanation component.
type ExplainFactor struct {
	Name   string  `json:"name,omitempty"`
	Value  float64 `json:"value,omitempty"`
	Detail string  `json:"detail,omitempty"`
}

// ExplainResponse contains an explanation of why a memory was relevant.
type ExplainResponse struct {
	EngramID string          `json:"engram_id,omitempty"`
	Summary  string          `json:"summary,omitempty"`
	Why      string          `json:"why,omitempty"`
	Factors  []ExplainFactor `json:"factors,omitempty"`
}

// ContradictionItem describes a contradiction pair or cluster reported by MuninnDB.
type ContradictionItem struct {
	ID         string   `json:"id,omitempty"`
	LeftID     string   `json:"left_id,omitempty"`
	RightID    string   `json:"right_id,omitempty"`
	LeftText   string   `json:"left_text,omitempty"`
	RightText  string   `json:"right_text,omitempty"`
	Reason     string   `json:"reason,omitempty"`
	Score      float64  `json:"score,omitempty"`
	EngramIDs  []string `json:"engram_ids,omitempty"`
	Summary    string   `json:"summary,omitempty"`
	Resolution string   `json:"resolution,omitempty"`
}

// ContradictionsResponse contains contradiction inspection results.
type ContradictionsResponse struct {
	Items []ContradictionItem `json:"items,omitempty"`
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
