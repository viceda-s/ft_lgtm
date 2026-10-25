FROM nginxinc/nginx-unprivileged:1.28-alpine
LABEL ft-lgtm=frontend
COPY nginx.conf /etc/nginx/conf.d/default.conf
COPY public/ /usr/share/nginx/html/
