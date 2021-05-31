FROM alpine:latest

ADD cdp-patch-admission-customizer /cdp-patch-admission-customizer
ENTRYPOINT ["./cdp-patch-admission-customizer"]