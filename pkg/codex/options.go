package codex

import "io"

type Option func(*clientOptions)

type clientOptions struct {
	binary              string
	args                []string
	workDir             string
	env                 map[string]string
	clientInfo          ClientInfo
	experimentalAPI     bool
	optOutNotifications []string
	notificationHandler NotificationHandler
	approvalHandler     ApprovalHandler
	stderrWriter        io.Writer
}

func WithBinary(path string) Option {
	return func(o *clientOptions) {
		o.binary = path
	}
}

func WithArgs(args ...string) Option {
	return func(o *clientOptions) {
		o.args = append([]string{}, args...)
	}
}

func WithWorkDir(dir string) Option {
	return func(o *clientOptions) {
		o.workDir = dir
	}
}

func WithEnv(env map[string]string) Option {
	return func(o *clientOptions) {
		if o.env == nil {
			o.env = make(map[string]string)
		}
		for key, value := range env {
			o.env[key] = value
		}
	}
}

func WithClientInfo(name, version string) Option {
	return func(o *clientOptions) {
		o.clientInfo.Name = name
		o.clientInfo.Version = version
	}
}

func WithClientTitle(title string) Option {
	return func(o *clientOptions) {
		o.clientInfo.Title = &title
	}
}

func WithExperimentalAPI(enabled bool) Option {
	return func(o *clientOptions) {
		o.experimentalAPI = enabled
	}
}

func WithOptOutNotifications(methods ...string) Option {
	return func(o *clientOptions) {
		o.optOutNotifications = append([]string{}, methods...)
	}
}

func WithNotificationHandler(handler NotificationHandler) Option {
	return func(o *clientOptions) {
		o.notificationHandler = handler
	}
}

func WithApprovalHandler(handler ApprovalHandler) Option {
	return func(o *clientOptions) {
		o.approvalHandler = handler
	}
}

func WithStderrWriter(writer io.Writer) Option {
	return func(o *clientOptions) {
		o.stderrWriter = writer
	}
}
