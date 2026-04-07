package oms

import "time"

// ObjectEmbedding stores a single vector embedding for a (objectTypeRID,
// primaryKey) tuple under a particular embedding model. Multiple models can
// coexist for the same object so we can A/B compare and migrate without
// losing data.
//
// The Embedding slice MUST contain exactly the dimension required by the
// schema column type (currently 1536 for ada-002 / text-embedding-3-small).
// Storing a different size will fail at INSERT time inside pgvector.
type ObjectEmbedding struct {
	ObjectTypeRID string    `json:"objectTypeRid"`
	PrimaryKey    string    `json:"primaryKey"`
	Embedding     []float32 `json:"embedding"`
	Model         string    `json:"model"`
	SourceText    string    `json:"sourceText,omitempty"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

// NearestNeighborResult is one row of a kNN query against object_embeddings.
// Distance is the cosine distance between the query vector and the stored
// embedding (0 = identical, 2 = opposite). Lower is closer.
type NearestNeighborResult struct {
	PrimaryKey string  `json:"primaryKey"`
	Distance   float32 `json:"distance"`
}
