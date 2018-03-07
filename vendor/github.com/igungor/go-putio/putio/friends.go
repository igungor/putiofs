package putio

import (
	"context"
	"fmt"
)

// FriendsService is the service to operate on user friends.
type FriendsService struct {
	client *Client
}

// Friend represents Put.io user's friend.
type Friend struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	AvatarURL string `json:"avatar_url"`
}

// List lists users friends.
func (f *FriendsService) List(ctx context.Context) ([]Friend, error) {
	req, err := f.client.NewRequest(ctx, "GET", "/v2/friends/list", nil)
	if err != nil {
		return nil, err
	}

	var r struct {
		Friends []Friend
		Total   int
	}
	_, err = f.client.Do(req, &r)
	if err != nil {
		return nil, err
	}

	return r.Friends, nil
}

// WaitingRequests lists user's pending friend requests.
func (f *FriendsService) WaitingRequests(ctx context.Context) ([]Friend, error) {
	req, err := f.client.NewRequest(ctx, "GET", "/v2/friends/waiting-requests", nil)
	if err != nil {
		return nil, err
	}

	var r struct {
		Friends []Friend
	}
	_, err = f.client.Do(req, &r)
	if err != nil {
		return nil, err
	}

	return r.Friends, nil
}

// Request sends a friend request to the given username.
func (f *FriendsService) Request(ctx context.Context, username string) error {
	if username == "" {
		return fmt.Errorf("empty username")
	}
	req, err := f.client.NewRequest(ctx, "POST", "/v2/friends/"+username+"/request", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	_, err = f.client.Do(req, &struct{}{})
	return err
}

// Approve approves a friend request from the given username.
func (f *FriendsService) Approve(ctx context.Context, username string) error {
	if username == "" {
		return fmt.Errorf("empty username")
	}

	req, err := f.client.NewRequest(ctx, "POST", "/v2/friends/"+username+"/approve", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	_, err = f.client.Do(req, &struct{}{})
	return err
}

// Deny denies a friend request from the given username.
func (f *FriendsService) Deny(ctx context.Context, username string) error {
	if username == "" {
		return fmt.Errorf("empty username")
	}

	req, err := f.client.NewRequest(ctx, "POST", "/v2/friends/"+username+"/deny", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	_, err = f.client.Do(req, &struct{}{})
	return err
}

// Unfriend removed friend from user's friend list.
func (f *FriendsService) Unfriend(ctx context.Context, username string) error {
	if username == "" {
		return fmt.Errorf("empty username")
	}

	req, err := f.client.NewRequest(ctx, "POST", "/v2/friends/"+username+"/unfriend", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	_, err = f.client.Do(req, &struct{}{})
	return err
}
