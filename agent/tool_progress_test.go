package agent

import "testing"

func TestExtractToolProgressEventFromStartItem(t *testing.T) {
	evt, ok := extractToolProgressEvent(&sessionUpdate{
		SessionUpdate: "tool_call_start",
		ToolCallStartItem: &toolCallStartItem{
			ToolName:   "exec",
			ToolCallID: "call-1",
		},
	})
	if !ok {
		t.Fatal("expected tool progress event")
	}
	if evt.Phase != "start" || evt.ToolName != "exec" || evt.ToolCallID != "call-1" {
		t.Fatalf("unexpected event: %+v", evt)
	}
}

func TestExtractToolProgressEventFromResultItem(t *testing.T) {
	evt, ok := extractToolProgressEvent(&sessionUpdate{
		SessionUpdate: "tool_call_result",
		ToolCallResultItem: &toolCallResultItem{
			ToolName:   "exec",
			ToolCallID: "call-1",
			Status:     "completed",
		},
	})
	if !ok {
		t.Fatal("expected tool progress event")
	}
	if evt.Phase != "end" || evt.Status != "completed" {
		t.Fatalf("unexpected event: %+v", evt)
	}
}

func TestExtractToolProgressEventNormalizesUnknownStatus(t *testing.T) {
	evt, ok := extractToolProgressEvent(&sessionUpdate{
		Phase:      "end",
		ToolName:   "exec",
		ToolCallID: "call-1",
		Status:     "weird",
	})
	if !ok {
		t.Fatal("expected tool progress event")
	}
	if evt.Status != "unknown" {
		t.Fatalf("status = %q, want unknown", evt.Status)
	}
}
