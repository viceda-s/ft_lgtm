FROM node:24.22.1-bookworm-slim AS monaco
WORKDIR /opt/monaco
RUN npm install --omit=dev --ignore-scripts --no-audit --no-fund monaco-editor@0.56.0

FROM nginxinc/nginx-unprivileged:1.28-alpine
LABEL ft-lgtm=frontend
COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY public/ /usr/share/nginx/html/
COPY --from=monaco /opt/monaco/node_modules/monaco-editor/min/vs /usr/share/nginx/html/vendor/monaco/min/vs
