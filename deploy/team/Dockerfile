# The Termwright team server as a container, built from the program in this
# folder (no internet needed):  docker build -t termwright-team .
FROM scratch
COPY termwright-team /termwright-team
VOLUME /data
EXPOSE 7443
ENTRYPOINT ["/termwright-team"]
