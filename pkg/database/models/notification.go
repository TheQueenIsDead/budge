package models

import (
	"encoding/json"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/database/buckets"
)

// NotificationLevel is how much the reader should care.
type NotificationLevel string

const (
	NotificationError   NotificationLevel = "error"
	NotificationWarning NotificationLevel = "warning"
	NotificationInfo    NotificationLevel = "info"
)

// Accent maps a level onto the palette, so a notification is the same colour
// wherever it appears.
func (l NotificationLevel) Accent() string {
	switch l {
	case NotificationError:
		return "brick"
	case NotificationWarning:
		return "clay"
	default:
		return "slate"
	}
}

// Icon marks a level, so the list can be scanned without reading every line.
func (l NotificationLevel) Icon() string {
	switch l {
	case NotificationError:
		return "bi-exclamation-octagon"
	case NotificationWarning:
		return "bi-exclamation-triangle"
	default:
		return "bi-info-circle"
	}
}

// Notification is something worth telling the owner about after the fact.
//
// It exists because a toast does not: a toast needs somebody looking at a page
// when it fires, and the things most worth reporting - a scheduled sync failing
// at 3am - happen when nobody is. This survives until it is read.
type Notification struct {
	Id      string            `json:"id"`
	Level   NotificationLevel `json:"level"`
	Title   string            `json:"title"`
	Message string            `json:"message"`

	// Source names what raised it, so a run of failures from one place reads as
	// one problem rather than several.
	Source string `json:"source"`

	CreatedAt time.Time `json:"created_at"`

	// ReadAt is zero until the list has been opened.
	ReadAt time.Time `json:"read_at"`
}

func (n Notification) Key() []byte    { return []byte(n.Id) }
func (n Notification) Bucket() []byte { return buckets.NotificationBucket }

func (n *Notification) Value() ([]byte, error) { return json.Marshal(n) }

// Unread reports whether the notification still needs the owner's attention.
func (n Notification) Unread() bool { return n.ReadAt.IsZero() }
