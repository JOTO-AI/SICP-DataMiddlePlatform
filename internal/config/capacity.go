package config

import (
	"fmt"
	"strconv"
	"time"
)

// Capacity settings are read only by the query process.
func (c *Runtime) loadCapacity(lookup func(string) (string, bool)) error {
	if c.Service.Role != Query {
		return nil
	}
	fields := []struct {
		key      string
		target   *int
		fallback int
	}{
		{"QUERY_DB_BUSINESS_MAX_CONNS", &c.QueryDBBusinessMaxConns, 16},
		{"QUERY_DB_CONTROL_MAX_CONNS", &c.QueryDBControlMaxConns, 16},
		{"QUERY_DB_LOG_MAX_CONNS", &c.QueryDBLogMaxConns, 16},
		{"QUERY_MAX_EXECUTIONS", &c.QueryMaxExecutions, 8},
		{"QUERY_MAX_QUEUE", &c.QueryMaxQueue, 64},
	}
	c.QueryExecutionTimeout = 30 * time.Second
	if raw, ok := lookup("QUERY_EXECUTION_TIMEOUT"); ok {
		value, err := time.ParseDuration(raw)
		if err != nil {
			return fmt.Errorf("QUERY_EXECUTION_TIMEOUT: duration required")
		}
		c.QueryExecutionTimeout = value
	}
	for _, field := range fields {
		*field.target = field.fallback
		if raw, ok := lookup(field.key); ok {
			value, err := strconv.Atoi(raw)
			if err != nil {
				return fmt.Errorf("%s: integer required", field.key)
			}
			*field.target = value
		}
	}
	return c.validateCapacity()
}

func (c Runtime) validateCapacity() error {
	if c.Service.Role != Query {
		return nil
	}
	for _, field := range []struct {
		key   string
		value int
	}{
		{"QUERY_DB_BUSINESS_MAX_CONNS", c.QueryDBBusinessMaxConns},
		{"QUERY_DB_CONTROL_MAX_CONNS", c.QueryDBControlMaxConns},
		{"QUERY_DB_LOG_MAX_CONNS", c.QueryDBLogMaxConns},
	} {
		if field.value < 1 || field.value > 1000 {
			return fmt.Errorf("%s: integer from 1 to 1000 required", field.key)
		}
	}
	if c.QueryMaxExecutions < 1 || c.QueryMaxExecutions > 10000 {
		return fmt.Errorf("QUERY_MAX_EXECUTIONS: integer from 1 to 10000 required")
	}
	if c.QueryMaxQueue < 0 || c.QueryMaxQueue > 100000 {
		return fmt.Errorf("QUERY_MAX_QUEUE: integer from 0 to 100000 required")
	}
	if c.QueryExecutionTimeout <= 0 || c.QueryExecutionTimeout > time.Hour {
		return fmt.Errorf("QUERY_EXECUTION_TIMEOUT: duration above zero and at most 1h required")
	}
	return nil
}
