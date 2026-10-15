package enablenow

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"
)

// Time is a timestamp from the enablenow API. The API returns timestamps both
// with a time zone and without one (e.g. "2026-10-15T11:48:53.9247493");
// zoneless timestamps are taken to be UTC.
type Time struct {
	time.Time
}

var timeLayouts = []string{
	time.RFC3339Nano,
	"2006-01-02T15:04:05.999999999",
	"2006-01-02",
}

func (t *Time) UnmarshalJSON(data []byte) error {
	if bytes.Equal(data, []byte("null")) {
		return nil
	}
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	for _, layout := range timeLayouts {
		if parsed, err := time.Parse(layout, s); err == nil {
			t.Time = parsed.UTC()
			return nil
		}
	}
	return fmt.Errorf("enablenow: cannot parse time %q", s)
}
