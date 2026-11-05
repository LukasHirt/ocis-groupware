package graphapi

import (
	"bytes"
	"encoding/json"
)

// eventJSON is a Graph event. Only the fields the facade maps are present.
type eventJSON struct {
	ID                   string          `json:"id"`
	ETag                 string          `json:"@odata.etag,omitempty"`
	ChangeKey            string          `json:"changeKey,omitempty"`
	ICalUID              string          `json:"iCalUId"`
	Type                 string          `json:"type"`
	SeriesMasterID       string          `json:"seriesMasterId,omitempty"`
	Subject              string          `json:"subject"`
	Body                 bodyJSON        `json:"body"`
	Start                dateTimeZone    `json:"start"`
	End                  dateTimeZone    `json:"end"`
	IsAllDay             bool            `json:"isAllDay"`
	IsCancelled          bool            `json:"isCancelled"`
	ShowAs               string          `json:"showAs"`
	Sensitivity          string          `json:"sensitivity"`
	Categories           []string        `json:"categories"`
	Attendees            []attendeeJSON  `json:"attendees"`
	Organizer            *recipientJSON  `json:"organizer,omitempty"`
	Recurrence           *recurrenceJSON `json:"recurrence"`
	RecurrenceEditable   *bool           `json:"groupware.recurrenceEditable,omitempty"`
	Location             locationJSON    `json:"location"`
	IsReminderOn         bool            `json:"isReminderOn"`
	ReminderMinutes      int             `json:"reminderMinutesBeforeStart"`
	CreatedDateTime      string          `json:"createdDateTime,omitempty"`
	LastModifiedDateTime string          `json:"lastModifiedDateTime,omitempty"`
}

type bodyJSON struct {
	ContentType string `json:"contentType"`
	Content     string `json:"content"`
}

type dateTimeZone struct {
	DateTime string `json:"dateTime"`
	TimeZone string `json:"timeZone"`
}

type locationJSON struct {
	DisplayName string `json:"displayName"`
}

type recipientJSON struct {
	EmailAddress emailAddress `json:"emailAddress"`
}

type attendeeJSON struct {
	Type         string         `json:"type"`
	Status       responseStatus `json:"status"`
	EmailAddress emailAddress   `json:"emailAddress"`
}

type responseStatus struct {
	Response string `json:"response"`
	Time     string `json:"time,omitempty"`
}

type recurrenceJSON struct {
	Pattern recurrencePattern `json:"pattern"`
	Range   recurrenceRange   `json:"range"`
}

type recurrencePattern struct {
	Type           string   `json:"type"`
	Interval       int      `json:"interval"`
	Month          int      `json:"month"`
	DayOfMonth     int      `json:"dayOfMonth"`
	DaysOfWeek     []string `json:"daysOfWeek"`
	FirstDayOfWeek string   `json:"firstDayOfWeek"`
	Index          string   `json:"index"`
}

type recurrenceRange struct {
	Type                string `json:"type"`
	StartDate           string `json:"startDate"`
	EndDate             string `json:"endDate,omitempty"`
	RecurrenceTimeZone  string `json:"recurrenceTimeZone,omitempty"`
	NumberOfOccurrences int    `json:"numberOfOccurrences"`
}

// opt is a JSON field that tells absent from null from a value, which a PATCH
// needs: an absent field is left alone and null clears it.
type opt[T any] struct {
	Set  bool
	Null bool
	Val  T
}

func (o *opt[T]) UnmarshalJSON(b []byte) error {
	o.Set = true
	if bytes.Equal(bytes.TrimSpace(b), []byte("null")) {
		o.Null = true
		return nil
	}
	return json.Unmarshal(b, &o.Val)
}

// eventPatch is the body of a POST or PATCH of an event.
type eventPatch struct {
	Subject         opt[string]         `json:"subject"`
	Body            opt[bodyJSON]       `json:"body"`
	Start           opt[dateTimeZone]   `json:"start"`
	End             opt[dateTimeZone]   `json:"end"`
	IsAllDay        opt[bool]           `json:"isAllDay"`
	ShowAs          opt[string]         `json:"showAs"`
	Sensitivity     opt[string]         `json:"sensitivity"`
	Categories      opt[[]string]       `json:"categories"`
	Attendees       opt[[]attendeeJSON] `json:"attendees"`
	Recurrence      opt[recurrenceJSON] `json:"recurrence"`
	Location        opt[locationJSON]   `json:"location"`
	IsReminderOn    opt[bool]           `json:"isReminderOn"`
	ReminderMinutes opt[int]            `json:"reminderMinutesBeforeStart"`
}
