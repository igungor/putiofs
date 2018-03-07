package putio

import "context"

// AccountService is the service to gather information about user account.
type AccountService struct {
	client *Client
}

// Settings represents user's personal settings.
type Settings struct {
	CallbackURL             string      `json:"callback_url"`
	DefaultDownloadFolder   int64       `json:"default_download_folder"`
	DefaultSubtitleLanguage string      `json:"default_subtitle_language"`
	DownloadFolderUnset     bool        `json:"download_folder_unset"`
	IsInvisible             bool        `json:"is_invisible"`
	Nextepisode             bool        `json:"nextepisode"`
	PrivateDownloadHostIP   interface{} `json:"private_download_host_ip"`
	PushoverToken           string      `json:"pushover_token"`
	Routing                 string      `json:"routing"`
	Sorting                 string      `json:"sorting"`
	SSLEnabled              bool        `json:"ssl_enabled"`
	StartFrom               bool        `json:"start_from"`
	SubtitleLanguages       []string    `json:"subtitle_languages"`
}

// AccountInfo represents user's account information.
type AccountInfo struct {
	AccountActive           bool   `json:"account_active"`
	AvatarURL               string `json:"avatar_url"`
	DaysUntilFilesDeletion  int    `json:"days_until_files_deletion"`
	DefaultSubtitleLanguage string `json:"default_subtitle_language"`
	Disk                    struct {
		Avail int64 `json:"avail"`
		Size  int64 `json:"size"`
		Used  int64 `json:"used"`
	} `json:"disk"`
	HasVoucher                bool     `json:"has_voucher"`
	Mail                      string   `json:"mail"`
	PlanExpirationDate        string   `json:"plan_expiration_date"`
	Settings                  Settings `json:"settings"`
	SimultaneousDownloadLimit int      `json:"simultaneous_download_limit"`
	SubtitleLanguages         []string `json:"subtitle_languages"`
	UserID                    int64    `json:"user_id"`
	Username                  string   `json:"username"`
}

// Info retrieves user account information.
func (a *AccountService) Info(ctx context.Context) (AccountInfo, error) {
	req, err := a.client.NewRequest(ctx, "GET", "/v2/account/info", nil)
	if err != nil {
		return AccountInfo{}, nil
	}

	var r struct {
		Info AccountInfo
	}
	_, err = a.client.Do(req, &r)
	if err != nil {
		return AccountInfo{}, err
	}
	return r.Info, nil
}

// Settings retrieves user preferences.
func (a *AccountService) Settings(ctx context.Context) (Settings, error) {
	req, err := a.client.NewRequest(ctx, "GET", "/v2/account/settings", nil)
	if err != nil {
		return Settings{}, nil
	}
	var r struct {
		Settings Settings
	}
	_, err = a.client.Do(req, &r)
	if err != nil {
		return Settings{}, err
	}

	return r.Settings, nil
}
