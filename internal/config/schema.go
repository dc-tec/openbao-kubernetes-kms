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
        "healthAddress": {"type": "string"}
      }
    },
    "openbao": {
      "type": "object",
      "additionalProperties": false,
      "required": ["address", "caCertFile", "tlsServerName", "instanceId"],
      "properties": {
        "address": {"type": "string", "format": "uri"},
        "namespace": {"type": "string", "pattern": "^$|^[^/%\\s]+(?:/[^/%\\s]+)*$"},
        "caCertFile": {"type": "string"},
        "tlsServerName": {"type": "string", "minLength": 1},
        "timeout": {"type": "string"},
        "instanceId": {"type": "string", "minLength": 1}
      }
    },
    "auth": {
      "type": "object",
      "additionalProperties": false,
      "required": ["method"],
      "properties": {
        "method": {"type": "string", "enum": ["jwt", "cert"]},
        "loginBeforeTokenExpiry": {"type": "string"},
        "tokenRenewalIncrement": {"type": "string"},
        "loginTimeout": {"type": "string"},
        "jwt": {
          "type": "object",
          "additionalProperties": false,
          "properties": {
            "mountPath": {"type": "string", "minLength": 1},
            "role": {"type": "string", "minLength": 1},
            "jwtFile": {"type": "string", "minLength": 1},
            "minRemainingTtl": {"type": "string"},
            "clockSkewLeeway": {"type": "string"},
            "expectedIssuer": {"type": "string"},
            "expectedAudience": {
              "type": "array",
              "items": {"type": "string", "minLength": 1}
            },
            "expectedSubject": {"type": "string"}
          }
        },
        "cert": {
          "type": "object",
          "additionalProperties": false,
          "properties": {
            "mountPath": {"type": "string", "minLength": 1},
            "name": {"type": "string"},
            "minRemainingTtl": {"type": "string"},
            "clockSkewLeeway": {"type": "string"},
            "source": {"type": "string", "enum": ["pkcs11", "spiffe"]},
            "pkcs11": {
              "type": "object",
              "additionalProperties": false,
              "properties": {
                "certificateFile": {"type": "string", "minLength": 1},
                "modulePath": {"type": "string", "minLength": 1},
                "tokenLabel": {"type": "string", "minLength": 1},
                "keyLabel": {"type": "string", "minLength": 1},
                "pinFile": {"type": "string", "minLength": 1},
                "maxSessions": {"type": "integer", "minimum": 2}
              }
            },
            "spiffe": {
              "type": "object",
              "additionalProperties": false,
              "properties": {
                "workloadAPISocket": {"type": "string", "pattern": "^unix:///.+"},
                "spiffeID": {"type": "string", "pattern": "^spiffe://[^/]+/.+"},
                "trustDomain": {"type": "string"}
              }
            }
          }
        },
        "mountPath": {"type": "string", "minLength": 1},
        "role": {"type": "string", "minLength": 1},
        "jwtFile": {"type": "string", "minLength": 1},
        "minJwtRemainingTtl": {"type": "string"},
        "clockSkewLeeway": {"type": "string"},
        "expectedIssuer": {"type": "string"},
        "expectedAudience": {
          "type": "array",
          "items": {"type": "string", "minLength": 1}
        },
        "expectedSubject": {"type": "string"}
	      }
	    },
    "transit": {
      "type": "object",
      "additionalProperties": false,
      "required": ["mountPath", "keyName", "keyIdScope"],
      "properties": {
        "mountPath": {"type": "string", "minLength": 1},
        "keyName": {"type": "string", "minLength": 1, "pattern": "^[^/%]+$"},
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
        }
      }
	    },
	    "bootstrap": {
	      "type": "object",
	      "additionalProperties": false,
	      "properties": {
	        "graceTimeout": {"type": "string"},
	        "retryInterval": {"type": "string"}
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
    "logging": {
      "type": "object",
      "additionalProperties": false,
      "properties": {
        "level": {"type": "string", "enum": ["debug", "info", "warn", "error"]},
        "format": {"type": "string", "enum": ["json", "text"]},
        "logOpenBaoRequestIDs": {"type": "boolean"},
        "debugCorrelation": {
          "type": "object",
          "additionalProperties": false,
          "properties": {
            "enabled": {"type": "boolean"},
            "ttl": {"type": "string"},
            "incidentId": {"type": "string", "maxLength": 64}
          }
        }
      }
    }
  }
}
`
