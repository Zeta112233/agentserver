// Package agentsdk provides a Go SDK for building custom agents that connect
// to agentserver via WebSocket tunnel.
//
// # Capabilities
//
// The SDK supports:
//   - OAuth Device Flow login (RequestDeviceCode, PollForToken)
//   - Agent registration and WebSocket+yamux tunnel connection
//   - HTTP request proxying via http.Handler
//   - Task polling (receive tasks assigned to this agent)
//   - Agent discovery (find other agents in the workspace)
//   - Task delegation (assign tasks to other agents)
//   - Async messaging (send/receive messages between agents)
//
// # Quick Start
//
//	package main
//
//	import (
//		"context"
//		"fmt"
//		"log"
//		"net/http"
//
//		"github.com/agentserver/agentserver/pkg/agentsdk"
//	)
//
//	func main() {
//		ctx := context.Background()
//		serverURL := "https://agent.example.com"
//
//		// 1. Authenticate via OAuth Device Flow.
//		deviceResp, err := agentsdk.RequestDeviceCode(ctx, serverURL)
//		if err != nil {
//			log.Fatal(err)
//		}
//		fmt.Printf("Visit: %s\n", deviceResp.VerificationURIComplete)
//
//		token, err := agentsdk.PollForToken(ctx, serverURL, deviceResp)
//		if err != nil {
//			log.Fatal(err)
//		}
//
//		// 2. Create client and register.
//		client := agentsdk.NewClient(agentsdk.Config{
//			ServerURL: serverURL,
//			Name:      "my-agent",
//			Type:      "custom",
//		})
//
//		reg, err := client.Register(ctx, token.AccessToken)
//		if err != nil {
//			log.Fatal(err)
//		}
//		fmt.Printf("Registered sandbox: %s\n", reg.SandboxID)
//
//		// 3. Connect with handlers.
//		err = client.Connect(ctx, agentsdk.Handlers{
//			HTTP: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
//				fmt.Fprintf(w, "Hello from custom agent!")
//			}),
//			Task: func(ctx context.Context, task *agentsdk.Task) error {
//				result := agentsdk.TaskResult{Output: "done"}
//				return task.Complete(ctx, result)
//			},
//			OnConnect: func() {
//				log.Println("Connected!")
//			},
//			OnDisconnect: func(err error) {
//				log.Printf("Disconnected: %v", err)
//			},
//		})
//		if err != nil {
//			log.Fatal(err)
//		}
//	}
//
// # Agent Interaction
//
// After connecting, agents can discover and interact with other agents:
//
//	// Discover other agents in the workspace.
//	agents, _ := client.DiscoverAgents(ctx)
//
//	// Delegate a task to another agent.
//	resp, _ := client.DelegateTask(ctx, agentsdk.DelegateTaskRequest{
//		TargetID: agents[0].AgentID,
//		Prompt:   "Review this code for security issues",
//		Skill:    "code_review",
//	})
//
//	// Wait for the task to complete.
//	result, _ := client.WaitForTask(ctx, resp.TaskID, 3*time.Second)
//
//	// Send a message to another agent.
//	client.SendMessage(ctx, agentsdk.SendMessageRequest{
//		To:   agents[0].AgentID,
//		Text: "Review complete, found 2 issues",
//	})
//
//	// Read incoming messages.
//	messages, _ := client.ReadInbox(ctx, 10)
package agentsdk
