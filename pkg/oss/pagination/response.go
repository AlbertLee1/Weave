package pagination

import "strconv"

// PageResponse wraps paginated results in the Palantir V2 format.
type PageResponse struct {
	Data          interface{} `json:"data"`
	NextPageToken string      `json:"nextPageToken,omitempty"`
	TotalCount    string      `json:"totalCount,omitempty"`
}

// NewPageResponse creates a PageResponse.
// It calculates whether there are more results to fetch.
func NewPageResponse(data interface{}, totalCount int, pageReq *PageRequest) *PageResponse {
	resp := &PageResponse{
		Data:       data,
		TotalCount: strconv.Itoa(totalCount),
	}

	nextOffset := pageReq.Cursor.Offset + pageReq.PageSize
	if nextOffset < totalCount {
		nextCursor := &Cursor{Offset: nextOffset}
		resp.NextPageToken = nextCursor.Encode()
	}

	return resp
}

// Apply applies pagination to a slice by returning the appropriate window.
// Returns the slice for the current page.
func Apply[T any](items []T, pageReq *PageRequest) []T {
	start := pageReq.Cursor.Offset
	if start >= len(items) {
		return []T{}
	}
	end := start + pageReq.PageSize
	if end > len(items) {
		end = len(items)
	}
	return items[start:end]
}
