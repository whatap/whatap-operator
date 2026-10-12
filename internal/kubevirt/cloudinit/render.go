// Package cloudinit renders secret-bearing first-boot configuration for new VMs.
// Render's output must only be stored in a Kubernetes Secret, never in inline
// VM userData, annotations, logs, errors, or process arguments.
package cloudinit

import (
	_ "embed"
	"errors"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Config supplies first-boot settings. Port zero uses 6600; an omitted
// AgentVersion uses DefaultAgentVersion. Version changes affect new VMs only.
type Config struct {
	License      string
	Host         string
	Port         int32
	AgentVersion string
}

const (
	scriptPath = "/usr/local/sbin/whatap-kubevirt-install.sh"
	configPath = "/etc/whatap-kubevirt/whatap.conf"
)

//go:embed bootstrap.sh
var bootstrapScript string

// Render preserves unrelated cloud-config settings and appends the bootstrap.
// The returned bytes contain the license; callers must treat them as secret.
func Render(existingUserData []byte, config Config) ([]byte, error) {
	if !validConfig(config) {
		return nil, errors.New("invalid server-agent bootstrap settings")
	}
	root, err := parseCloudConfig(existingUserData)
	if err != nil {
		return nil, err
	}
	if config.Port == 0 {
		config.Port = 6600
	}
	if config.AgentVersion == "" {
		config.AgentVersion = DefaultAgentVersion
	}
	script := strings.ReplaceAll(bootstrapScript, "__WHATAP_AGENT_VERSION__", config.AgentVersion)
	// Values are validated as single-line data and encoded by YAML, not evaluated
	// by a shell. Preserve them literally for the native agent's config parser.
	settings := "license=" + config.License + "\nwhatap.server.host=" + config.Host + "\nwhatap.server.port=" + strconv.FormatInt(int64(config.Port), 10) + "\n"
	for _, file := range []map[string]string{
		{"path": scriptPath, "permissions": "0700", "owner": "root:root", "content": script},
		{"path": configPath, "permissions": "0600", "owner": "root:root", "content": settings},
	} {
		var entry yaml.Node
		if err := entry.Encode(file); err != nil {
			return nil, errors.New("cannot encode bootstrap")
		}
		appendList(root, "write_files", &entry)
	}
	var command yaml.Node
	if err := command.Encode([]string{"/bin/bash", scriptPath}); err != nil {
		return nil, errors.New("cannot encode bootstrap command")
	}
	appendList(root, "runcmd", &command)
	root.HeadComment = ""
	data, err := yaml.Marshal(root)
	if err != nil {
		return nil, errors.New("cannot encode cloud-config")
	}
	return append([]byte("#cloud-config\n"), data...), nil
}

func appendList(root *yaml.Node, key string, entry *yaml.Node) {
	for i := 0; i < len(root.Content); i += 2 {
		if root.Content[i].Value == key {
			root.Content[i+1].Content = append(root.Content[i+1].Content, entry)
			return
		}
	}
	root.Content = append(root.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq", Content: []*yaml.Node{entry}})
}
