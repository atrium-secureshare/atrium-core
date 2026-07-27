package provider

import (
	"log/slog"
	"time"
)

// nextcloudAudience is the aud claim the Nextcloud plugin's JWT validator pins.
// It lives here, with the backend it belongs to, not in the shared REST base.
const nextcloudAudience = "atrium-plugin-nextcloud"

// NewNextcloud builds the Nextcloud backend: the shared REST base with the aud the
// Nextcloud plugin requires. It is the template for a new backend — copy this
// file, change the audience, and (only if a call differs) embed *client to
// override that one method.
func NewNextcloud(baseURL, privateKeyPEM string, streamTimeout time.Duration, logger *slog.Logger) (Service, error) {
	return newClient(baseURL, privateKeyPEM, nextcloudAudience, streamTimeout, logger)
}
