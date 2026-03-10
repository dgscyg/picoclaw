package muninndb

import "time"

// Association represents a relationship between two engrams.
type Association struct {
	TargetID   [16]byte `json:"target_id"`
	RelType    uint16   `json:"rel_type"`
	Weight     float32  `json:"weight"`
	Confidence float32  `json:"confidence"`
}

// Engram is the core MuninnDB memory record.
type Engram struct {
	ID           [16]byte      `json:"id"`
	Content      string        `json:"content"`
	Tags         []string      `json:"tags,omitempty"`
	Associations []Association `json:"associations,omitempty"`
	Embedding    []float32     `json:"embedding,omitempty"`
	Concept      string        `json:"concept,omitempty"`
	Summary      string        `json:"summary,omitempty"`
	KeyPoints    []string      `json:"key_points,omitempty"`
	CreatedAt    time.Time     `json:"created_at,omitempty"`
	Confidence   float32       `json:"confidence,omitempty"`
	Relevance    float32       `json:"relevance,omitempty"`
}

// ActivateRequest describes a semantic activation query.
type ActivateRequest struct {
	Query string `json:"query"`
	Limit int    `json:"limit,omitempty"`
	Mode  string `json:"mode,omitempty"`
}

// ActivateResponse contains activation results.
type ActivateResponse struct {
	Engrams []Engram `json:"engrams"`
}
