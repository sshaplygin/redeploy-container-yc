package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"go.uber.org/zap"
)

var logger *zap.Logger

func init() {
	logger, _ = zap.NewProduction()
}

const (
	metadataTokenURL = "http://169.254.169.254/computeMetadata/v1/instance/service-accounts/default/token"
	containersAPIURL = "https://serverless-containers.api.cloud.yandex.net/containers/v1"
)

// TriggerEvent is the payload sent by a Container Registry trigger.
type TriggerEvent struct {
	Messages []CRMessage `json:"messages"`
}

type CRMessage struct {
	EventMetadata CREventMetadata `json:"event_metadata"`
	Details       CRDetails       `json:"details"`
}

type CREventMetadata struct {
	EventID   string `json:"event_id"`
	EventType string `json:"event_type"`
	CreatedAt string `json:"created_at"`
	CloudID   string `json:"cloud_id"`
	FolderID  string `json:"folder_id"`
}

type CRDetails struct {
	RegistryID     string `json:"registry_id"`
	RepositoryName string `json:"repository_name"`
	Tag            string `json:"tag"`
	ImageID        string `json:"image_id"`
	ImageDigest    string `json:"image_digest"`
}

const (
	eventTypeCreateImage    = "yandex.cloud.events.containerregistry.CreateImage"
	eventTypeCreateImageTag = "yandex.cloud.events.containerregistry.CreateImageTag"
)

type iamTokenResponse struct {
	AccessToken string `json:"access_token"`
}

type container struct {
	Status struct {
		ActiveRevisionID string `json:"activeRevisionId"`
	} `json:"status"`
}

// revision mirrors the fields returned by GET /revisions/{id} that we
// need to pass back verbatim when deploying a new revision.
type revision struct {
	Resources        json.RawMessage `json:"resources"`
	ExecutionTimeout string          `json:"executionTimeout,omitempty"`
	Concurrency      int64           `json:"concurrency,omitempty"`
	ServiceAccountID string          `json:"serviceAccountId,omitempty"`
	Image            revisionImage   `json:"image"`
	Secrets          json.RawMessage `json:"secrets,omitempty"`
	Connectivity     json.RawMessage `json:"connectivity,omitempty"`
	LogOptions       json.RawMessage `json:"logOptions,omitempty"`
}

type revisionImage struct {
	ImageURL   string            `json:"imageUrl"`
	Command    []string          `json:"command,omitempty"`
	Args       []string          `json:"args,omitempty"`
	Env        map[string]string `json:"environment,omitempty"`
	WorkingDir string            `json:"workingDir,omitempty"`
}

type deployRevisionRequest struct {
	ContainerID      string          `json:"containerId"`
	Resources        json.RawMessage `json:"resources"`
	ExecutionTimeout string          `json:"executionTimeout,omitempty"`
	Concurrency      int64           `json:"concurrency,omitempty"`
	ServiceAccountID string          `json:"serviceAccountId,omitempty"`
	Image            revisionImage   `json:"image"`
	Secrets          json.RawMessage `json:"secrets,omitempty"`
	Connectivity     json.RawMessage `json:"connectivity,omitempty"`
	LogOptions       json.RawMessage `json:"logOptions,omitempty"`
}

// Handler is the entry point for the Yandex Cloud Function.
// It receives a Container Registry push event, resolves the target
// Serverless Container from IMAGE_CONTAINER_MAP, and deploys a new
// revision with the updated image.
func Handler(_ context.Context, event TriggerEvent) (string, error) {
	if len(event.Messages) == 0 {
		return "", fmt.Errorf("empty messages")
	}

	msg := event.Messages[0]
	et := msg.EventMetadata.EventType
	if et != eventTypeCreateImage && et != eventTypeCreateImageTag {
		logger.Info("ignored event type", zap.String("event_type", et))
		return fmt.Sprintf(`{"status":"ignored","event_type":%q}`, et), nil
	}

	d := msg.Details
	if d.RegistryID == "" || d.RepositoryName == "" || d.Tag == "" {
		return "", fmt.Errorf("missing registry event details")
	}

	imageURL := fmt.Sprintf("cr.yandex/%s/%s:%s", d.RegistryID, d.RepositoryName, d.Tag)
	logger.Info("push event received", zap.String("image", imageURL), zap.String("event_type", et))

	containerMap, err := parseContainerMap(os.Getenv("IMAGE_CONTAINER_MAP"))
	if err != nil {
		return "", fmt.Errorf("container map error: %w", err)
	}

	containerID, ok := containerMap[d.RepositoryName]
	if !ok {
		logger.Info("no container mapped, skipping", zap.String("repository", d.RepositoryName))
		return fmt.Sprintf(`{"status":"skipped","image":%q}`, imageURL), nil
	}

	token, err := getIAMToken()
	if err != nil {
		return "", fmt.Errorf("get IAM token: %w", err)
	}

	rev, err := getCurrentRevision(token, containerID)
	if err != nil {
		return "", fmt.Errorf("get revision: %w", err)
	}

	rev.Image.ImageURL = imageURL

	if err := deployRevision(token, containerID, rev); err != nil {
		return "", fmt.Errorf("deploy revision: %w", err)
	}

	logger.Info("deployed", zap.String("image", imageURL), zap.String("container_id", containerID))
	return fmt.Sprintf(`{"status":"ok","image":%q,"container":%q}`, imageURL, containerID), nil
}

func parseContainerMap(raw string) (map[string]string, error) {
	if raw == "" {
		return nil, fmt.Errorf("IMAGE_CONTAINER_MAP env var is not set")
	}
	var m map[string]string
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return nil, fmt.Errorf("invalid JSON in IMAGE_CONTAINER_MAP: %w", err)
	}
	return m, nil
}

func getIAMToken() (string, error) {
	req, err := http.NewRequest(http.MethodGet, metadataTokenURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Metadata-Flavor", "Google")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	var t iamTokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&t); err != nil {
		return "", err
	}
	return t.AccessToken, nil
}

func getCurrentRevision(token, containerID string) (*revision, error) {
	// 1. Fetch container to get the active revision ID.
	var c container
	if err := apiGet(token, fmt.Sprintf("%s/containers/%s", containersAPIURL, containerID), &c); err != nil {
		return nil, fmt.Errorf("get container: %w", err)
	}

	revisionID := c.Status.ActiveRevisionID
	if revisionID == "" {
		return nil, fmt.Errorf("container %s has no active revision", containerID)
	}

	// 2. Fetch full revision config.
	var rev revision
	if err := apiGet(token, fmt.Sprintf("%s/revisions/%s", containersAPIURL, revisionID), &rev); err != nil {
		return nil, fmt.Errorf("get revision: %w", err)
	}
	return &rev, nil
}

func deployRevision(token, containerID string, rev *revision) error {
	payload := deployRevisionRequest{
		ContainerID:      containerID,
		Resources:        rev.Resources,
		ExecutionTimeout: rev.ExecutionTimeout,
		Concurrency:      rev.Concurrency,
		ServiceAccountID: rev.ServiceAccountID,
		Image:            rev.Image,
		Secrets:          rev.Secrets,
		Connectivity:     rev.Connectivity,
		LogOptions:       rev.LogOptions,
	}

	data, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal request: %w", err)
	}

	url := fmt.Sprintf("%s/revisions:deployRevision", containersAPIURL)
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("deploy request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("read resp body: %v", err)
		}

		return fmt.Errorf("status %d: %s", resp.StatusCode, body)
	}

	return nil
}

func apiGet(token, url string, dst any) error {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("read respone body %v", err)
		}

		return fmt.Errorf("status %d: %s", resp.StatusCode, body)
	}
	return json.NewDecoder(resp.Body).Decode(dst)
}
