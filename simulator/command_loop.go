package simulator

import "context"

// CommandSource provides control commands from an external producer.
type CommandSource[T any] interface {
	NextCommand() (T, bool)
	WaitCommand(context.Context) (T, bool)
}

// CommandHandler consumes control commands and determines whether processing should continue.
type CommandHandler[T any] interface {
	HandleCommand(T) bool
}

// CommandHandlerFunc adapts a function into a CommandHandler.
type CommandHandlerFunc[T any] func(T) bool

// HandleCommand calls the underlying function.
func (f CommandHandlerFunc[T]) HandleCommand(cmd T) bool {
	if f == nil {
		return true
	}
	return f(cmd)
}

// CommandLoop drains and dispatches control commands.
type CommandLoop[T any] struct {
	source  CommandSource[T]
	handler CommandHandler[T]
}

// NewCommandLoop creates a command loop with the given source and handler.
func NewCommandLoop[T any](source CommandSource[T], handler CommandHandler[T]) *CommandLoop[T] {
	return &CommandLoop[T]{
		source:  source,
		handler: handler,
	}
}

// DrainPending pulls all currently available commands from the source until exhaustion or handler termination.
func (c *CommandLoop[T]) DrainPending() bool {
	if c == nil || c.handler == nil {
		return true
	}
	if c.source == nil {
		return true
	}
	for {
		cmd, ok := c.source.NextCommand()
		if !ok {
			return true
		}
		if !c.handler.HandleCommand(cmd) {
			return false
		}
	}
}

// WaitAndHandle blocks until a command is available (or context cancellation) and dispatches it.
func (c *CommandLoop[T]) WaitAndHandle(ctx context.Context) bool {
	if c == nil || c.handler == nil {
		return true
	}
	if c.source == nil {
		return true
	}
	cmd, ok := c.source.WaitCommand(ctx)
	if !ok {
		return true
	}
	return c.handler.HandleCommand(cmd)
}

