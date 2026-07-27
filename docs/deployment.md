# Deployment

Atrium Core is a single stateless container configured by environment variables
([full reference](configuration.md)). It needs an OIDC provider for recipient
login and a storage plugin
([atrium-plugin-nextcloud](https://github.com/atrium-secureshare/atrium-plugin-nextcloud))
reachable on the internal network; neither the plugin nor its storage faces the
internet.

## Trust key (once)

The core signs plugin requests with an ES256 JWT. The private half goes to the
core, the public half onto the plugin (see the plugin docs):

```bash
openssl ecparam -name prime256v1 -genkey -noout -out provider-signing.key
openssl ec -in provider-signing.key -pubout -out provider-signing.pub
```

Until the plugin trusts the key, `/readyz` returns `503` (core up but degraded)
while `/healthz` stays `200`.

## Docker Compose

```yaml
services:
  atrium-core:
    image: ghcr.io/atrium-secureshare/atrium-core
    ports:
      - "8080:8080"
    environment:
      OIDC_ISSUER: https://keycloak.example.org/realms/atrium-external
      OIDC_CLIENT_ID: atrium-core
      OIDC_CLIENT_SECRET: change-me
      OIDC_REDIRECT_URI: https://atrium.example.org/auth/callback
      SESSION_KEY: change-me            # openssl rand -base64 32
      PROVIDER_TYPE: nextcloud
      PROVIDER_BASE_URL: http://nextcloud/apps/atrium_secureshare
      PROVIDER_JWT_PRIVATE_KEY: ${PROVIDER_JWT_PRIVATE_KEY}
    restart: unless-stopped
```

```bash
export PROVIDER_JWT_PRIVATE_KEY="$(cat provider-signing.key)"
docker compose up
```

## Kubernetes

```bash
kubectl create secret generic atrium-core \
  --from-literal=OIDC_CLIENT_SECRET=change-me \
  --from-literal=SESSION_KEY="$(openssl rand -base64 32)" \
  --from-file=PROVIDER_JWT_PRIVATE_KEY=provider-signing.key
```

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: atrium-core
spec:
  replicas: 2
  selector:
    matchLabels:
      app: atrium-core
  template:
    metadata:
      labels:
        app: atrium-core
    spec:
      securityContext:
        runAsNonRoot: true
      containers:
        - name: atrium-core
          image: ghcr.io/atrium-secureshare/atrium-core
          ports:
            - name: http
              containerPort: 8080
          env:
            - name: OIDC_ISSUER
              value: https://keycloak.example.org/realms/atrium-external
            - name: OIDC_CLIENT_ID
              value: atrium-core
            - name: OIDC_REDIRECT_URI
              value: https://atrium.example.org/auth/callback
            - name: PROVIDER_TYPE
              value: nextcloud
            - name: PROVIDER_BASE_URL
              value: http://nextcloud/apps/atrium_secureshare
            - name: OIDC_CLIENT_SECRET
              valueFrom:
                secretKeyRef:
                  name: atrium-core
                  key: OIDC_CLIENT_SECRET
            - name: SESSION_KEY
              valueFrom:
                secretKeyRef:
                  name: atrium-core
                  key: SESSION_KEY
            - name: PROVIDER_JWT_PRIVATE_KEY
              valueFrom:
                secretKeyRef:
                  name: atrium-core
                  key: PROVIDER_JWT_PRIVATE_KEY
          securityContext:
            readOnlyRootFilesystem: true
            allowPrivilegeEscalation: false
            capabilities:
              drop:
                - ALL
          readinessProbe:
            httpGet:
              path: /readyz
              port: http
          livenessProbe:
            httpGet:
              path: /healthz
              port: http
---
apiVersion: v1
kind: Service
metadata:
  name: atrium-core
spec:
  selector:
    app: atrium-core
  ports:
    - port: 8080
      targetPort: http
```

Expose the Service through your own ingress (TLS in front). Optional
Terms-of-Service and branding mounts: see [configuration.md](configuration.md).
