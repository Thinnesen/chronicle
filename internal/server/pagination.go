package server

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"time"

	"connectrpc.com/connect"
)

// maxPage is the default and the largest page size of a list.
const maxPage = 500

// cursor is the position after the last run of a page. Runs are sorted by creation time, newest first, then by name.
// A cursor makes each page cost the same, also deep in a large archive.
type cursor struct {
	Time time.Time `json:"t"`
	Name string    `json:"n"`
}

// parseCursor reads a page token. An empty token is the first page.
func parseCursor(token string) (*cursor, error) {
	if token == "" {
		return nil, nil
	}
	data, err := base64.RawURLEncoding.DecodeString(token)
	var c cursor
	if err != nil || json.Unmarshal(data, &c) != nil || c.Name == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("bad page token"))
	}
	return &c, nil
}

func (c cursor) token() string {
	data, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(data)
}

// before tells if a run comes before the cursor, so that it was on an earlier page.
func (c *cursor) before(created time.Time, name string) bool {
	if c == nil {
		return false
	}
	return created.After(c.Time) || created.Equal(c.Time) && name <= c.Name
}

// pageSize returns the page size of a request.
func pageSize(limit int32) int {
	if limit <= 0 || limit > maxPage {
		return maxPage
	}
	return int(limit)
}
