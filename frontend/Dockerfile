# Stage 1: Build the SvelteKit static SPA
FROM node:22-alpine AS builder
WORKDIR /app
COPY package.json package-lock.json* ./
RUN npm ci
COPY . .
# Pass WebSocket URL as build arg for production
ARG VITE_WS_URL=""
ARG VITE_API_URL=""
ENV VITE_WS_URL=$VITE_WS_URL
ENV VITE_API_URL=$VITE_API_URL
RUN npm run build

# Stage 2: Serve with nginx (non-root)
FROM nginx:1.27-alpine

LABEL maintainer="Whisper Team"
LABEL description="Whisper anonymous real-time chat frontend"
LABEL version="1.0.0"

# Remove default nginx config
RUN rm /etc/nginx/conf.d/default.conf

# Prepare nginx to run as non-root
# - Create writable directories for pid, cache, and logs
# - Update main nginx.conf to write pid to a non-root-writable location
# - Remove the "user" directive (not allowed when running as non-root)
RUN sed -i 's|/var/run/nginx.pid|/tmp/nginx.pid|' /etc/nginx/nginx.conf && \
    sed -i '/^user  nginx;/d' /etc/nginx/nginx.conf && \
    chown -R nginx:nginx /var/cache/nginx /var/log/nginx && \
    chmod -R 755 /var/cache/nginx /var/log/nginx && \
    touch /tmp/nginx.pid && chown nginx:nginx /tmp/nginx.pid

# Add rate-limiting zone (must be in http context, loaded before server blocks)
RUN printf 'limit_req_zone $binary_remote_addr zone=general:10m rate=10r/s;\n' \
    > /etc/nginx/conf.d/00-rate-limit.conf

COPY --from=builder /app/build /usr/share/nginx/html
COPY nginx.conf /etc/nginx/conf.d/default.conf

# Ensure the nginx user can read the static files
RUN chown -R nginx:nginx /usr/share/nginx/html

USER nginx

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
    CMD wget -qO /dev/null http://localhost:8080/ || exit 1

CMD ["nginx", "-g", "daemon off;"]
