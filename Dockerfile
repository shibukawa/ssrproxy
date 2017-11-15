From alpine:edge

RUN apk add --update udev ttf-freefont chromium

ENTRYPOINT tail -f /dev/null
