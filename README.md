# peroapi

CLI tool that transforms a GraphQL persisted operations map and schema into a fully typed OpenAPI v3 YAML specification. Designed for teams running persisted-only GraphQL APIs with RPC-style endpoints.

Each persisted operation becomes its own HTTP route (`{base-path}/{OperationName}`), with request variables and response selection sets resolved against the schema and emitted as typed OpenAPI schemas.

## Installation

```sh
go install github.com/Thomasparsley/peroapi@latest
```

Or build from source:

```sh
git clone https://github.com/Thomasparsley/peroapi.git
cd peroapi
go build -o peroapi .
```

## Usage

```sh
peroapi generate --schema <schema.graphql> --operations <gql-persisted.json> [flags]
```

### Flags

| Flag | Short | Default | Description |
| --- | --- | --- | --- |
| `--schema` | `-s` | — | Path to the GraphQL schema (**required**) |
| `--operations` | `-o` | — | Path to the persisted operations map JSON (**required**) |
| `--output` | | _(prompt)_ | Path to write the OpenAPI YAML. If omitted, you're asked whether to print to stdout |
| `--method-convention` | | `rest` | HTTP method strategy: `rest`, `post`, or `get` |
| `--base-path` | | `/api/graphql` | Path prefix for every generated route |
| `--title` | | `Generated API` | Value for OpenAPI `info.title` |
| `--version` | | `1.0.0` | Value for OpenAPI `info.version` |

### Method conventions

- **`rest`** (default) — queries map to `GET`, mutations map to `POST`.
- **`post`** — every operation maps to `POST`.
- **`get`** — every operation maps to `GET`.

For `GET` routes, operation variables become individual query parameters. For `POST` routes, variables are packed into a JSON request body under a `variables` object.

## Input formats

**Persisted operations map** (`gql-persisted.json`) — a flat object mapping operation names to their GraphQL document strings:

```json
{
  "GetUser": "query GetUser($id: ID!) { user(id: $id) { id name email } }",
  "CreateUser": "mutation CreateUser($input: CreateUserInput!) { createUser(input: $input) { id name } }"
}
```

**Schema** (`schema.graphql`) — a standard GraphQL SDL file defining the types referenced by your operations:

```graphql
input CreateUserInput {
  name: String!
  email: String!
}

type User {
  id: ID!
  name: String!
  email: String!
}

type Query {
  user(id: ID!): User
}

type Mutation {
  createUser(input: CreateUserInput!): User!
}
```

## Examples

Generate a spec using the default REST convention and write it to a file:

```sh
peroapi generate \
  --schema schema.graphql \
  --operations gql-persisted.json \
  --output openapi.yaml
```

Generate with every operation forced to `POST`. With no `--output`, peroapi prompts whether to print to stdout:

```sh
peroapi generate -s schema.graphql -o gql-persisted.json --method-convention post
# No output file specified. Print to stdout? [Y/n]:
```

Customize the route prefix and API metadata:

```sh
peroapi generate \
  -s schema.graphql \
  -o gql-persisted.json \
  --base-path /v1/rpc \
  --title "Orders API" \
  --version 2.3.0 \
  --output orders-openapi.yaml
```

### Example output

With the `rest` convention, a query like `GetUser` becomes a `GET` route (variables as query parameters) and a mutation like `CreateUser` becomes a `POST` route (variables in the request body):

```yaml
openapi: 3.0.3
info:
    title: Generated API
    version: 1.0.0
paths:
    /api/graphql/GetUser:
        get:
            operationId: GetUser
            parameters:
                - name: id
                  in: query
                  required: true
                  schema:
                    type: string
            responses:
                "200":
                    description: Successful response
                    # ... typed data + errors object
    /api/graphql/CreateUser:
        post:
            operationId: CreateUser
            requestBody:
                required: true
                content:
                    application/json:
                        schema:
                            type: object
                            properties:
                                variables:
                                    type: object
                                    properties:
                                        input:
                                            $ref: '#/components/schemas/CreateUserInput'
            responses:
                "200":
                    description: Successful response
                    # ... typed data + errors object
```

## License

See [LICENSE](LICENSE).
