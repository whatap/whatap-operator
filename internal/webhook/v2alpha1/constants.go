package v2alpha1

const (
	// Whatap Server Connection Env Vars (Operator side)
	EnvWhatapLicense = "WHATAP_LICENSE"
	EnvWhatapHost    = "WHATAP_HOST"
	EnvWhatapPort    = "WHATAP_PORT"

	// Downward API Env Vars
	EnvNodeIP   = "NODE_IP"
	EnvNodeName = "NODE_NAME"
	EnvPodName  = "POD_NAME"

	// Common Agent Env Vars
	EnvWhatapMicroEnabled = "whatap.micro.enabled"
	ValTrue               = "true"

	// Java Agent Constants
	EnvJavaLicense           = "license"
	EnvJavaWhatapHost        = "whatap.server.host"
	EnvJavaWhatapPort        = "whatap.server.port"
	EnvJavaAgentPath         = "WHATAP_JAVA_AGENT_PATH"
	EnvJavaToolOptions       = "JAVA_TOOL_OPTIONS"
	ValJavaAgentPath         = "/whatap-agent/whatap.agent.java.jar"
	ValJavaAgentOptionPrefix = "-javaagent:"

	// Python Agent Constants
	EnvPythonLicense    = "license"
	EnvPythonWhatapHost = "whatap_server_host"
	EnvPythonWhatapPort = "whatap_server_port"
	EnvPythonAgentPath  = "WHATAP_PYTHON_AGENT_PATH"
	EnvWhatapHome       = "WHATAP_HOME"
	EnvAppName          = "app_name"
	EnvAppProcessName   = "app_process_name"
	EnvOkind            = "OKIND"
	EnvPythonPath       = "PYTHONPATH"

	ValWhatapHome      = "/whatap-agent"
	ValPythonBootstrap = "/whatap-agent/whatap/bootstrap"
	ValPythonAgentPath = "/whatap-agent/whatap_python"

	// Node.js Agent Constants
	EnvNodejsLicense    = "WHATAP_LICENSE"
	EnvNodejsWhatapHost = "WHATAP_SERVER_HOST"
	EnvNodejsWhatapPort = "WHATAP_SERVER_PORT"
	EnvNodejsAgentPath  = "WHATAP_NODEJS_AGENT_PATH"
	EnvNodejsOptions    = "NODE_OPTIONS"
	EnvNodejsPath       = "NODE_PATH"
	ValNodejsAgentPath  = "/whatap-agent/node_modules/whatap/agent/whatap_nodejs"
	ValNodejsModules    = "/whatap-agent/node_modules"
	ValNodejsRequire    = "-r whatap"

	// Init Container
	InitContainerName     = "whatap-agent-init"
	VolumeNameWhatapAgent = "whatap-agent-volume"
	MountPathWhatapAgent  = "/whatap-agent"
)
