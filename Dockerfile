FROM cimg/go:1.19.1
USER circleci
RUN mkdir -p /home/circleci/app
WORKDIR /home/circleci/app
COPY --chown=circleci:circleci *.go go.mod go.sum ./
EXPOSE 3333
RUN go build
CMD [ "./ipfs-check" ]
