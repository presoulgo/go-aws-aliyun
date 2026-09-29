package notify

import "context"

// sendWebhook POSTs the message as JSON; any 2xx response counts as success.
func sendWebhook(ctx context.Context, t Target, msg Message) error {
	if msg.Items == nil {
		msg.Items = []Item{}
	}
	_, err := postJSON(ctx, t.URL, msg)
	return err
}
