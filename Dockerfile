# FROM        scratch
FROM        gcr.io/distroless/static-debian12

ENTRYPOINT  ["/zinigo"]

ADD        ./distribution/zinigo-linux-amd64 /zinigo