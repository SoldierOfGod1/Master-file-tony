# =============================================================================
# Frontend Dockerfile — SOLDIER OF GOD Command Centre
# Multi-stage Node build with nginx runtime
# =============================================================================

FROM node:22-alpine AS builder

WORKDIR /app

# Cache dependencies
COPY package.json package-lock.json ./
RUN npm ci --ignore-scripts

# Copy source and build
COPY . .
RUN npm run build

# ---------------------------------------------------------------------------
# Runtime stage — nginx for static serving
# ---------------------------------------------------------------------------
FROM nginx:1.27-alpine

LABEL maintainer="baptista.manuel@rain.co.za"
LABEL org.opencontainers.image.source="https://github.com/SoldierOfGod1/command-centre"
LABEL org.opencontainers.image.description="Command Centre Web UI"

# Remove default nginx config
RUN rm /etc/nginx/conf.d/default.conf

# Copy build output (Vite outputs to dist/ by default)
COPY --from=builder /app/dist /usr/share/nginx/html

# Copy custom nginx config
COPY docker/frontend/nginx.conf /etc/nginx/conf.d/default.conf

EXPOSE 80

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget -qO- http://localhost/health || exit 1

CMD ["nginx", "-g", "daemon off;"]
