FROM alpine

WORKDIR imperius
COPY imperius .
COPY test_scripts ./test_scripts
ENTRYPOINT ["./imperius", "./test_scripts"]