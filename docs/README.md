# API documentation

`openapi.yaml` contains the OpenAPI 3.1 description of the benchmark HTTP API.
It documents the default configuration in `config/config.json` and can be
opened with Swagger Editor or Swagger UI.

To run Swagger UI locally with Docker from the project root:

```sh
docker run --rm -p 8081:8080 \
  -e SWAGGER_JSON=/spec/openapi.yaml \
  -v "$PWD/docs:/spec:ro" \
  swaggerapi/swagger-ui
```

Open <http://localhost:8081> after the container starts. The API server itself
must be running on port 8080 before using **Try it out**.
