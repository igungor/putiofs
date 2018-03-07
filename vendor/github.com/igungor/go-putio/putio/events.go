package putio

import "context"

// EventsService is the service to gather information about user's events.
type EventsService struct {
	client *Client
}

// Event represents a Put.io event. It could be a transfer or a shared file.
type Event struct {
	ID           int64  `json:"id"`
	FileID       int64  `json:"file_id"`
	Source       string `json:"source"`
	Type         string `json:"type"`
	TransferName string `json:"transfer_name"`
	TransferSize int64  `json:"transfer_size"`
	CreatedAt    *Time  `json:"created_at"`
}

// FIXME: events list returns inconsistent data structures.

// List gets list of dashboard events. It includes downloads and share events.
func (e *EventsService) List(ctx context.Context) ([]Event, error) {
	req, err := e.client.NewRequest(ctx, "GET", "/v2/events/list", nil)
	if err != nil {
		return nil, err
	}

	var r struct {
		Events []Event
	}
	_, err = e.client.Do(req, &r)
	if err != nil {
		return nil, err
	}
	return r.Events, nil

}

// Delete clears all dashboard events.
func (e *EventsService) Delete(ctx context.Context) error {
	req, err := e.client.NewRequest(ctx, "POST", "/v2/events/delete", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	_, err = e.client.Do(req, &struct{}{})
	return err
}
