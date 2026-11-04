# hello-world

A JSON endpoint packaged as an AWS Lambda container image, sized to show the whole
Rocketsled loop in one sitting.

```
GET /hello?name=sled
{"message":"hello, world","path":"/hello","method":"GET","version":"<commit>","time":"…","query":{"name":"sled"}}
```

Link this repository as a stack with path `example/hello-world` and a project whose
app component is `aws-lambda-function`. Every push builds this directory, pushes the
image tagged with the commit, and redeploys the function with `image_tag` set to it.

Local check:

```sh
go test ./...
docker build --platform linux/amd64 --provenance=false -t hello-world:local .
```

Set `GREETING` in the function's environment to change the message without a rebuild.
