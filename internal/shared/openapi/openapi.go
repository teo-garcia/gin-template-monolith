// Package openapi builds the service's OpenAPI 3.0 document.
//
// The document is assembled in Go rather than generated from annotations by a
// codegen binary. That keeps `make build` free of a codegen step, and — more
// importantly — it makes it possible to document the *envelope* rather than the
// inner payload. A spec that describes the bare `Task` would be wrong: no
// endpoint ever returns one unwrapped.
package openapi

import (
	"github.com/teo-garcia/gin-template-monolith/internal/config"
	"github.com/teo-garcia/gin-template-monolith/internal/modules/tasks"
)

// Document is the root OpenAPI object.
type Document struct {
	OpenAPI    string         `json:"openapi"`
	Info       Info           `json:"info"`
	Servers    []Server       `json:"servers"`
	Tags       []Tag          `json:"tags"`
	Paths      map[string]any `json:"paths"`
	Components Components     `json:"components"`
}

// Info describes the API.
type Info struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Version     string `json:"version"`
}

// Server is one base URL the API is served from.
type Server struct {
	URL         string `json:"url"`
	Description string `json:"description"`
}

// Tag groups related operations.
type Tag struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// Components holds the reusable schema objects.
type Components struct {
	Schemas map[string]any `json:"schemas"`
}

// Build assembles the document for the running configuration.
func Build(cfg config.Config) Document {
	prefix := cfg.App.APIPrefix

	return Document{
		OpenAPI: "3.0.3",
		Info: Info{
			Title: cfg.App.Name,
			Description: "Gin monolith template. Every 2xx body is wrapped in the " +
				"portfolio success envelope and every 4xx/5xx body in the error envelope.",
			Version: cfg.App.Version,
		},
		Servers: []Server{{URL: cfg.App.OpenAPIServer, Description: cfg.App.Env}},
		Tags: []Tag{
			{Name: "Tasks", Description: "Task CRUD with pagination and soft delete"},
			{Name: "System", Description: "Health, metrics, and service info"},
		},
		Paths: map[string]any{
			"/":                    rootPath(),
			"/health":              healthPath("Aggregate health", "Reports every dependency."),
			"/health/live":         livePath(),
			"/health/ready":        healthPath("Readiness", "Reports whether the service can serve traffic."),
			prefix + "/tasks":      tasksCollectionPath(),
			prefix + "/tasks/{id}": taskItemPath(),
		},
		Components: Components{Schemas: schemas()},
	}
}

func rootPath() map[string]any {
	return map[string]any{
		"get": map[string]any{
			"tags":        []string{"System"},
			"summary":     "Service info",
			"operationId": "getServiceInfo",
			"responses": map[string]any{
				"200": envelopedResponse("Service metadata", ref("AppInfo")),
			},
		},
	}
}

func livePath() map[string]any {
	return map[string]any{
		"get": map[string]any{
			"tags":        []string{"System"},
			"summary":     "Liveness",
			"description": "Does not touch dependencies, so a database outage cannot cause a restart loop.",
			"operationId": "getLiveness",
			"responses": map[string]any{
				"200": jsonResponse("Process is alive", map[string]any{
					"type":       "object",
					"properties": map[string]any{"status": map[string]any{"type": "string", "example": "ok"}},
				}),
			},
		},
	}
}

func healthPath(summary, description string) map[string]any {
	return map[string]any{
		"get": map[string]any{
			"tags":        []string{"System"},
			"summary":     summary,
			"description": description + " Health responses are not enveloped: orchestrators parse them directly.",
			"operationId": "get" + summary,
			"responses": map[string]any{
				"200": jsonResponse("All dependencies healthy", ref("HealthResponse")),
				"503": jsonResponse("At least one dependency is unhealthy", ref("HealthResponse")),
			},
		},
	}
}

func tasksCollectionPath() map[string]any {
	return map[string]any{
		"get": map[string]any{
			"tags":        []string{"Tasks"},
			"summary":     "List tasks with pagination",
			"operationId": "listTasks",
			"parameters": []any{
				queryParam("status", "Filter by status", map[string]any{
					"type": "string", "enum": statusEnum(),
				}),
				queryParam("priority", "Filter by priority", map[string]any{
					"type": "integer", "minimum": 0, "maximum": tasks.MaxPriority,
				}),
				queryParam("page", "1-based page number", map[string]any{
					"type": "integer", "minimum": 1, "default": tasks.DefaultPage,
				}),
				queryParam("pageSize", "Items per page", map[string]any{
					"type": "integer", "minimum": 1,
					"maximum": tasks.MaxPageSize, "default": tasks.DefaultPageSize,
				}),
			},
			"responses": map[string]any{
				"200": envelopedResponse("Paginated tasks", ref("PaginatedTasks")),
				"422": errorResponse("Invalid query parameters"),
				"429": errorResponse("Rate limit exceeded"),
			},
		},
		"post": map[string]any{
			"tags":        []string{"Tasks"},
			"summary":     "Create a task",
			"operationId": "createTask",
			"requestBody": requestBody(ref("CreateTaskRequest")),
			"responses": map[string]any{
				"201": envelopedResponse("Task created", ref("Task")),
				"400": errorResponse("Malformed request body"),
				"422": errorResponse("Validation failed"),
				"429": errorResponse("Rate limit exceeded"),
			},
		},
	}
}

func taskItemPath() map[string]any {
	idParam := map[string]any{
		"name": "id", "in": "path", "required": true,
		"description": "Task identifier",
		"schema":      map[string]any{"type": "string", "format": "uuid"},
	}

	return map[string]any{
		"parameters": []any{idParam},
		"get": map[string]any{
			"tags":        []string{"Tasks"},
			"summary":     "Get a task by id",
			"operationId": "getTask",
			"responses": map[string]any{
				"200": envelopedResponse("Task found", ref("Task")),
				"404": errorResponse("Task not found"),
			},
		},
		"patch": map[string]any{
			"tags":        []string{"Tasks"},
			"summary":     "Update a task",
			"operationId": "updateTask",
			"requestBody": requestBody(ref("UpdateTaskRequest")),
			"responses": map[string]any{
				"200": envelopedResponse("Task updated", ref("Task")),
				"400": errorResponse("Malformed request body"),
				"404": errorResponse("Task not found"),
				"422": errorResponse("Validation failed"),
			},
		},
		"delete": map[string]any{
			"tags":        []string{"Tasks"},
			"summary":     "Soft-delete a task",
			"operationId": "deleteTask",
			"responses": map[string]any{
				// 204 carries no body, so it is documented without a schema
				// rather than with an empty envelope.
				"204": map[string]any{"description": "Task deleted"},
				"404": errorResponse("Task not found"),
			},
		},
	}
}

// envelopedResponse documents a 2xx as the success envelope with `data`
// narrowed to the given schema. This is the allOf wrapper that keeps the spec
// honest about what clients actually receive.
func envelopedResponse(description string, dataSchema map[string]any) map[string]any {
	return jsonResponse(description, map[string]any{
		"allOf": []any{
			ref("SuccessEnvelope"),
			map[string]any{
				"type":       "object",
				"properties": map[string]any{"data": dataSchema},
			},
		},
	})
}

func errorResponse(description string) map[string]any {
	return jsonResponse(description, ref("ErrorEnvelope"))
}

func jsonResponse(description string, schema map[string]any) map[string]any {
	return map[string]any{
		"description": description,
		"content": map[string]any{
			"application/json": map[string]any{"schema": schema},
		},
	}
}

func requestBody(schema map[string]any) map[string]any {
	return map[string]any{
		"required": true,
		"content": map[string]any{
			"application/json": map[string]any{"schema": schema},
		},
	}
}

func queryParam(name, description string, schema map[string]any) map[string]any {
	return map[string]any{
		"name": name, "in": "query", "required": false,
		"description": description, "schema": schema,
	}
}

func ref(name string) map[string]any {
	return map[string]any{"$ref": "#/components/schemas/" + name}
}

func statusEnum() []any {
	all := tasks.AllStatuses()
	out := make([]any, len(all))
	for i, s := range all {
		out[i] = string(s)
	}
	return out
}

func schemas() map[string]any {
	return map[string]any{
		"SuccessEnvelope": map[string]any{
			"type":     "object",
			"required": []string{"success", "statusCode", "timestamp", "path", "method", "data", "meta"},
			"properties": map[string]any{
				"success":    map[string]any{"type": "boolean", "example": true},
				"statusCode": map[string]any{"type": "integer", "example": 200},
				"timestamp":  map[string]any{"type": "string", "format": "date-time"},
				"path":       map[string]any{"type": "string", "example": "/api/v1/tasks"},
				"method":     map[string]any{"type": "string", "example": "GET"},
				"data":       map[string]any{"description": "Operation payload"},
				"meta": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"requestId": map[string]any{"type": "string"},
						"version":   map[string]any{"type": "string", "example": "1"},
						"duration":  map[string]any{"type": "string", "example": "1.2ms"},
					},
				},
			},
		},
		"ErrorEnvelope": map[string]any{
			"type":     "object",
			"required": []string{"success", "statusCode", "timestamp", "path", "method", "message", "error"},
			"properties": map[string]any{
				"success":    map[string]any{"type": "boolean", "example": false},
				"statusCode": map[string]any{"type": "integer", "example": 404},
				"timestamp":  map[string]any{"type": "string", "format": "date-time"},
				"path":       map[string]any{"type": "string"},
				"method":     map[string]any{"type": "string"},
				"message":    map[string]any{"type": "string", "description": "Client-safe message"},
				"error": map[string]any{
					"type":        "string",
					"description": "Stable machine-readable error class",
					"enum": []any{
						"ValidationError", "BadRequestError", "UnauthorizedError",
						"ForbiddenError", "NotFoundError", "ConflictError",
						"RateLimitError", "TimeoutError", "DatabaseError",
						"InternalServerError", "MethodNotAllowedError",
					},
				},
				"errors": map[string]any{
					"type":                 "object",
					"description":          "Field-level validation detail",
					"additionalProperties": map[string]any{"type": "string"},
				},
				"meta": map[string]any{
					"type":       "object",
					"properties": map[string]any{"requestId": map[string]any{"type": "string"}},
				},
			},
		},
		"Task": map[string]any{
			"type":     "object",
			"required": []string{"id", "title", "status", "priority", "createdAt", "updatedAt"},
			"properties": map[string]any{
				"id":          map[string]any{"type": "string", "format": "uuid"},
				"title":       map[string]any{"type": "string", "minLength": 1, "maxLength": 200},
				"description": map[string]any{"type": "string", "nullable": true, "maxLength": 2000},
				"status":      map[string]any{"type": "string", "enum": statusEnum()},
				"priority":    map[string]any{"type": "integer", "minimum": 0, "maximum": tasks.MaxPriority},
				"createdAt":   map[string]any{"type": "string", "format": "date-time"},
				"updatedAt":   map[string]any{"type": "string", "format": "date-time"},
				"deletedAt":   map[string]any{"type": "string", "format": "date-time", "nullable": true},
			},
		},
		"PaginatedTasks": map[string]any{
			"type":     "object",
			"required": []string{"data", "meta"},
			"properties": map[string]any{
				"data": map[string]any{"type": "array", "items": ref("Task")},
				"meta": map[string]any{
					"type":     "object",
					"required": []string{"total", "page", "pageSize"},
					"properties": map[string]any{
						"total":    map[string]any{"type": "integer"},
						"page":     map[string]any{"type": "integer"},
						"pageSize": map[string]any{"type": "integer"},
					},
				},
			},
		},
		"CreateTaskRequest": map[string]any{
			"type":     "object",
			"required": []string{"title"},
			"properties": map[string]any{
				"title":       map[string]any{"type": "string", "minLength": 1, "maxLength": 200},
				"description": map[string]any{"type": "string", "maxLength": 2000},
				"status":      map[string]any{"type": "string", "enum": statusEnum(), "default": "PENDING"},
				"priority":    map[string]any{"type": "integer", "minimum": 0, "maximum": tasks.MaxPriority, "default": 0},
			},
		},
		"UpdateTaskRequest": map[string]any{
			"type":          "object",
			"description":   "Partial update; supply at least one field.",
			"minProperties": 1,
			"properties": map[string]any{
				"title":       map[string]any{"type": "string", "minLength": 1, "maxLength": 200},
				"description": map[string]any{"type": "string", "maxLength": 2000},
				"status":      map[string]any{"type": "string", "enum": statusEnum()},
				"priority":    map[string]any{"type": "integer", "minimum": 0, "maximum": tasks.MaxPriority},
			},
		},
		"AppInfo": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"name":    map[string]any{"type": "string"},
				"version": map[string]any{"type": "string"},
				"env":     map[string]any{"type": "string"},
				"docs":    map[string]any{"type": "string"},
			},
		},
		"HealthResponse": map[string]any{
			"type": "object",
			"properties": map[string]any{
				"status":  map[string]any{"type": "string", "enum": []any{"ok", "error"}},
				"info":    map[string]any{"type": "object", "additionalProperties": true},
				"error":   map[string]any{"type": "object", "additionalProperties": true},
				"details": map[string]any{"type": "object", "additionalProperties": true},
			},
		},
	}
}
