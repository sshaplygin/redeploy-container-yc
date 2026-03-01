package main

import (
	"bytes"
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
	Messages []struct {
		Details struct {
			RegistryID     string `json:"registry_id"`
			RepositoryName string `json:"repository_name"`
			Tag            string `json:"tag"`
		} `json:"details"`
	} `json:"messages"`
}

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
func Handler(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		logger.Error("failed to read body", zap.Error(err))
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	var event TriggerEvent
	if err := json.Unmarshal(body, &event); err != nil || len(event.Messages) == 0 {
		logger.Error("invalid event payload", zap.Error(err), zap.Int("messages_count", len(event.Messages)))
		http.Error(w, "invalid event payload", http.StatusBadRequest)
		return
	}

	d := event.Messages[0].Details
	if d.RegistryID == "" || d.RepositoryName == "" || d.Tag == "" {
		http.Error(w, "missing registry event details", http.StatusBadRequest)
		return
	}

	imageURL := fmt.Sprintf("cr.yandex/%s/%s:%s", d.RegistryID, d.RepositoryName, d.Tag)
	logger.Info("push event received", zap.String("image", imageURL))

	containerMap, err := parseContainerMap(os.Getenv("IMAGE_CONTAINER_MAP"))
	if err != nil {
		logger.Error("container map error", zap.Error(err))
		http.Error(w, fmt.Sprintf("container map error: %v", err), http.StatusInternalServerError)
		return
	}

	containerID, ok := containerMap[d.RepositoryName]
	if !ok {
		logger.Info("no container mapped, skipping", zap.String("repository", d.RepositoryName))
		fmt.Fprintf(w, `{"status":"skipped","image":%q}`, imageURL)
		return
	}

	token, err := getIAMToken()
	if err != nil {
		logger.Error("get IAM token", zap.Error(err))
		http.Error(w, fmt.Sprintf("get IAM token: %v", err), http.StatusInternalServerError)
		return
	}

	rev, err := getCurrentRevision(token, containerID)
	if err != nil {
		logger.Error("get revision", zap.Error(err), zap.String("container_id", containerID))
		http.Error(w, fmt.Sprintf("get revision: %v", err), http.StatusInternalServerError)
		return
	}

	rev.Image.ImageURL = imageURL

	if err := deployRevision(token, containerID, rev); err != nil {
		logger.Error("deploy revision", zap.Error(err), zap.String("container_id", containerID), zap.String("image", imageURL))
		http.Error(w, fmt.Sprintf("deploy revision: %v", err), http.StatusInternalServerError)
		return
	}

	logger.Info("deployed", zap.String("image", imageURL), zap.String("container_id", containerID))
	fmt.Fprintf(w, `{"status":"ok","image":%q,"container":%q}`, imageURL, containerID)
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
		body, _ := io.ReadAll(resp.Body)
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
