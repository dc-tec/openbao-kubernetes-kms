package config

// SchemaJSON returns the provider configuration JSON Schema used by docs and tooling.
func SchemaJSON() []byte {
	return []byte(configSchemaJSON)
}

const configSchemaJSON = `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "$id": "https://github.com/dc-tec/openbao-kubernetes-kms/config.schema.json",
  "title": "bao-kms-provider configuration",
  "type": "object",
  "additionalProperties": false,
  "required": ["server", "openbao", "auth", "transit"],
  "properties": {
    "configVersion": {"type": "string", "const": "v1alpha1"},
    "server": {
      "type": "object",
      "additionalProperties": false,
      "required": ["socketPath", "socketMode", "socketGroup"],
      "properties": {
        "socketPath": {"type": "string"},
        "socketMode": {"type": "string", "pattern": "^0[0-7]{3}$"},
        "socketGroup": {"type": "string", "minLength": 1},
        "metricsAddress": {"type": "string"},
        "healthAddress": {"type": "string"},
        "adminAddress": {"type": "string"},
        "unsafeDebugEndpoints": {"type": "boolean"}
      }
    },
    "openbao": {
      "type": "object",
      "additionalProperties": false,
      "required": ["address", "caCertFile", "tlsServerName", "instanceId"],
      "properties": {
        "address": {"type": "string", "format": "uri"},
        "namespace": {"type": "string"},
        "caCertFile": {"type": "string"},
        "tlsServerName": {"type": "string", "minLength": 1},
        "timeout": {"type": "string"},
        "instanceId": {"type": "string", "minLength": 1}
      }
    },
    "auth": {
      "type": "object",
      "additionalProperties": false,
      "required": ["method", "mountPath", "role", "jwtFile"],
      "properties": {
        "method": {"type": "string", "const": "jwt"},
        "mountPath": {"type": "string", "minLength": 1},
        "role": {"type": "string", "minLength": 1},
        "jwtFile": {"type": "string", "minLength": 1},
        "minJwtRemainingTtl": {"type": "string"},
        "clockSkewLeeway": {"type": "string"},
        "loginBeforeTokenExpiry": {"type": "string"},
        "tokenStorage": {"type": "string", "const": "memory"}
      }
    },
    "transit": {
      "type": "object",
      "additionalProperties": false,
      "required": ["mountPath", "keyName", "keyIdScope"],
      "properties": {
        "mountPath": {"type": "string", "minLength": 1},
        "keyName": {"type": "string", "minLength": 1},
        "keyIdScope": {
          "type": "object",
          "additionalProperties": false,
          "required": ["providerName", "clusterId", "transitMountId", "keyLineageId"],
          "properties": {
            "providerName": {"type": "string", "minLength": 1},
            "clusterId": {"type": "string", "minLength": 1},
            "transitMountId": {"type": "string", "minLength": 1},
            "keyLineageId": {"type": "string", "minLength": 1}
          }
        },
        "useAssociatedData": {"type": "boolean", "const": true}
      }
    },
    "status": {
      "type": "object",
      "additionalProperties": false,
      "properties": {
        "probeInterval": {"type": "string"},
        "deepProbeInterval": {"type": "string"},
        "statusMaxStaleness": {"type": "string"}
      }
    },
    "state": {
      "type": "object",
      "additionalProperties": false,
      "properties": {
        "path": {"type": "string"}
      }
    },
    "rotation": {
      "type": "object",
      "additionalProperties": false,
      "properties": {
        "mode": {"type": "string", "const": "observed"},
        "activationDelay": {"type": "string"},
        "requireStableObservationCount": {"type": "integer", "minimum": 1},
        "rejectVersionRollback": {"type": "boolean"}
      }
    },
    "performance": {
      "type": "object",
      "additionalProperties": false,
      "properties": {
        "decryptMicroBatching": {
          "type": "object",
          "additionalProperties": false,
          "properties": {
            "enabled": {"type": "boolean"},
            "maxBatchSize": {"type": "integer", "minimum": 1},
            "maxWait": {"type": "string"}
          }
        }
      }
    },
    "logging": {
      "type": "object",
      "additionalProperties": false,
      "properties": {
        "level": {"type": "string", "enum": ["debug", "info", "warn", "error"]},
        "format": {"type": "string", "enum": ["json", "text"]},
        "redactOpenBaoPaths": {"type": "boolean"},
        "logOpenBaoRequestIDs": {"type": "boolean"}
      }
    }
  }
}
`
