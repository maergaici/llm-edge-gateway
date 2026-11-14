FROM alpine:3.20
COPY server /server
RUN chmod +x /server
EXPOSE 8080
ENTRYPOINT ["/server"]
