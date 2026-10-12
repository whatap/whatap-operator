package v2alpha1

import (
	"fmt"
	"strings"

	monitoringv2alpha1 "github.com/whatap/whatap-operator/api/v2alpha1"
	corev1 "k8s.io/api/core/v1"
)

const nodejsNpmPreloadPath = "/whatap-agent/whatap-preload.cjs"

const nodejsNpmPreload = `const fs = require('node:fs');
const path = require('node:path');
let npm = false;
if (process.argv[1]) {
  try {
    const entry = fs.realpathSync(process.argv[1]);
    if (path.basename(entry) === 'npm-cli.js' && path.basename(path.dirname(entry)) === 'bin') {
      const pkg = JSON.parse(fs.readFileSync(path.join(path.dirname(entry), '..', 'package.json'), 'utf8'));
      npm = pkg.name === 'npm';
    }
  } catch {
    // An unidentified entry point remains instrumented.
  }
}
if (!npm) require('whatap');
`

func skipNodejsNpm(target monitoringv2alpha1.TargetSpec) bool {
	for _, env := range target.Envs {
		if env.Name == "WHATAP_SKIP_NPM" {
			return env.Value == "true"
		}
	}
	return false
}

func configureNodejsNpmPreload(containers []corev1.Container) {
	if len(containers) == 0 {
		return
	}
	c := &containers[0]
	content := strings.ReplaceAll(nodejsNpmPreload, "'", "'\"'\"'")
	write := fmt.Sprintf("printf '%%s' '%s' > %s", content, nodejsNpmPreloadPath)
	if len(c.Command) == 0 {
		c.Command = []string{"sh", "-c"}
		c.Args = []string{"if [ -x /init.sh ]; then /init.sh; fi"}
	}
	c.Args[0] += " && " + write
}
