package putio

import (
	"context"
	"fmt"
	"net/url"
	"strings"
)

// TransfersService is the service to operate on torrent transfers, such as
// adding a torrent or magnet link, retrying a current one etc.
type TransfersService struct {
	client *Client
}

// Transfer represents a Put.io transfer state.
type Transfer struct {
	Availability   int    `json:"availability"`
	CallbackURL    string `json:"callback_url"`
	CreatedAt      *Time  `json:"created_at"`
	CreatedTorrent bool   `json:"created_torrent"`
	ClientIP       string `json:"client_ip"`

	// FIXME: API returns either string or float non-deterministically.
	// CurrentRatio       float32 `json:"current_ratio"`

	DownloadSpeed      int    `json:"down_speed"`
	Downloaded         int64  `json:"downloaded"`
	DownloadID         int64  `json:"download_id"`
	ErrorMessage       string `json:"error_message"`
	EstimatedTime      int64  `json:"estimated_time"`
	Extract            bool   `json:"extract"`
	FileID             int64  `json:"file_id"`
	FinishedAt         *Time  `json:"finished_at"`
	ID                 int64  `json:"id"`
	IsPrivate          bool   `json:"is_private"`
	MagnetURI          string `json:"magneturi"`
	Name               string `json:"name"`
	PeersConnected     int    `json:"peers_connected"`
	PeersGettingFromUs int    `json:"peers_getting_from_us"`
	PeersSendingToUs   int    `json:"peers_sending_to_us"`
	PercentDone        int    `json:"percent_done"`
	SaveParentID       int64  `json:"save_parent_id"`
	SecondsSeeding     int    `json:"seconds_seeding"`
	Size               int    `json:"size"`
	Source             string `json:"source"`
	Status             string `json:"status"`
	StatusMessage      string `json:"status_message"`
	SubscriptionID     int    `json:"subscription_id"`
	TorrentLink        string `json:"torrent_link"`
	TrackerMessage     string `json:"tracker_message"`
	Trackers           string `json:"tracker"`
	Type               string `json:"type"`
	UploadSpeed        int    `json:"up_speed"`
	Uploaded           int64  `json:"uploaded"`
}

// List lists all active transfers. If a transfer is completed, it will not be
// available in response.
func (t *TransfersService) List(ctx context.Context) ([]Transfer, error) {
	req, err := t.client.NewRequest(ctx, "GET", "/v2/transfers/list", nil)
	if err != nil {
		return nil, err
	}

	var r struct {
		Transfers []Transfer
	}
	_, err = t.client.Do(req, &r)
	if err != nil {
		return nil, err
	}

	return r.Transfers, nil
}

// Add creates a new transfer. A valid torrent or a magnet URL is expected.
// Parent is the folder where the new transfer is downloaded to. If a negative
// value is given, user's preferred download folder is used. CallbackURL is
// used to send a POST request after the transfer is finished downloading.
func (t *TransfersService) Add(ctx context.Context, urlStr string, parent int64, callbackURL string) (Transfer, error) {
	if urlStr == "" {
		return Transfer{}, fmt.Errorf("empty URL")
	}

	params := url.Values{}
	params.Set("url", urlStr)
	// negative values indicate user's preferred download folder. don't include
	// it in the request
	if parent >= 0 {
		params.Set("save_parent_id", itoa(parent))
	}
	if callbackURL != "" {
		params.Set("callback_url", callbackURL)
	}

	req, err := t.client.NewRequest(ctx, "POST", "/v2/transfers/add", strings.NewReader(params.Encode()))
	if err != nil {
		return Transfer{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	var r struct {
		Transfer Transfer
	}
	_, err = t.client.Do(req, &r)
	if err != nil {
		return Transfer{}, err
	}

	return r.Transfer, nil
}

// Get returns the given transfer's properties.
func (t *TransfersService) Get(ctx context.Context, id int64) (Transfer, error) {
	if id < 0 {
		return Transfer{}, errNegativeID
	}

	req, err := t.client.NewRequest(ctx, "GET", "/v2/transfers/"+itoa(id), nil)
	if err != nil {
		return Transfer{}, err
	}

	var r struct {
		Transfer Transfer
	}
	_, err = t.client.Do(req, &r)
	if err != nil {
		return Transfer{}, err
	}

	return r.Transfer, nil
}

// Retry retries previously failed transfer.
func (t *TransfersService) Retry(ctx context.Context, id int64) (Transfer, error) {
	if id < 0 {
		return Transfer{}, errNegativeID
	}

	params := url.Values{}
	params.Set("id", itoa(id))

	req, err := t.client.NewRequest(ctx, "POST", "/v2/transfers/retry", strings.NewReader(params.Encode()))
	if err != nil {
		return Transfer{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	var r struct {
		Transfer Transfer
	}
	_, err = t.client.Do(req, &r)
	if err != nil {
		return Transfer{}, err
	}

	return r.Transfer, nil
}

// Cancel deletes given transfers.
func (t *TransfersService) Cancel(ctx context.Context, ids ...int64) error {
	if len(ids) == 0 {
		return fmt.Errorf("no id given")
	}

	var transfers []string
	for _, id := range ids {
		if id < 0 {
			return errNegativeID
		}
		transfers = append(transfers, itoa(id))
	}

	params := url.Values{}
	params.Set("transfer_ids", strings.Join(transfers, ","))

	req, err := t.client.NewRequest(ctx, "POST", "/v2/transfers/cancel", strings.NewReader(params.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	_, err = t.client.Do(req, &struct{}{})
	return err
}

// Clean removes completed transfers from the transfer list.
func (t *TransfersService) Clean(ctx context.Context) error {
	req, err := t.client.NewRequest(ctx, "POST", "/v2/transfers/clean", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	_, err = t.client.Do(req, &struct{}{})
	return err
}
