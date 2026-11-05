package graphapi

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"time"
)

// IDs
//
// A calendar or contact folder id is the unpadded base64url form of the
// groupkit collection id, the same text for the owner and for everyone the
// collection is shared with. An event or contact id is that id, a dot and the
// unpadded base64url form of the object's name. An occurrence id is an event
// id, a dot and the occurrence's recurrence id in UTC. The base64url alphabet
// has no dot, so the parts split unambiguously.

// recurrenceIDLayout is the form of a recurrence id inside an occurrence id.
const recurrenceIDLayout = "20060102T150405Z"

var (
	b64 = base64.RawURLEncoding.Strict()

	errBadID = errors.New("malformed id")
)

func encodePart(s string) string { return b64.EncodeToString([]byte(s)) }

func decodePart(s string) (string, error) {
	// The decoder skips carriage returns and line feeds, which would give one
	// name several ids.
	if s == "" || strings.ContainsAny(s, "\r\n") {
		return "", errBadID
	}
	b, err := b64.DecodeString(s)
	if err != nil || len(b) == 0 {
		return "", errBadID
	}
	return string(b), nil
}

// CalendarID returns the Graph id of a collection.
func CalendarID(collectionID string) string { return encodePart(collectionID) }

// ParseCalendarID returns the collection id a Graph calendar or contact
// folder id stands for.
func ParseCalendarID(id string) (string, error) {
	if strings.Contains(id, ".") {
		return "", errBadID
	}
	return decodePart(id)
}

// EventID returns the Graph id of an object in a collection.
func EventID(collectionID, objectName string) string {
	return encodePart(collectionID) + "." + encodePart(objectName)
}

// ParseEventID returns the collection id and the object name of an event or
// contact id.
func ParseEventID(id string) (collectionID, objectName string, err error) {
	coll, name, ok := strings.Cut(id, ".")
	if !ok || strings.Contains(name, ".") {
		return "", "", errBadID
	}
	if collectionID, err = decodePart(coll); err != nil {
		return "", "", err
	}
	if objectName, err = decodePart(name); err != nil {
		return "", "", err
	}
	return collectionID, objectName, nil
}

// OccurrenceID returns the id of the occurrence of an event whose
// RECURRENCE-ID is recurrenceID.
func OccurrenceID(eventID string, recurrenceID time.Time) string {
	return eventID + "." + recurrenceID.UTC().Format(recurrenceIDLayout)
}

// ParseOccurrenceID splits an occurrence id into its event id and recurrence
// id. The recurrence id is in UTC.
func ParseOccurrenceID(id string) (eventID string, recurrenceID time.Time, err error) {
	i := strings.LastIndex(id, ".")
	if i < 0 {
		return "", time.Time{}, errBadID
	}
	eventID = id[:i]
	if _, _, err := ParseEventID(eventID); err != nil {
		return "", time.Time{}, err
	}
	recurrenceID, err = time.ParseInLocation(recurrenceIDLayout, id[i+1:], time.UTC)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("%w: recurrence id", errBadID)
	}
	if recurrenceID.Format(recurrenceIDLayout) != id[i+1:] {
		return "", time.Time{}, errBadID
	}
	return eventID, recurrenceID, nil
}
