package telegram

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"rizznet/internal/collectors"
	"rizznet/internal/logger"
	"rizznet/internal/xray" 

	"github.com/gotd/td/telegram"
	"github.com/gotd/td/telegram/auth"
	"github.com/gotd/td/telegram/dcs"
	"github.com/gotd/td/tg"
	"golang.org/x/net/proxy"
)

type TelegramCollector struct{}

// Collect runs the Telegram Userbot to scrape messages.
func (c *TelegramCollector) Collect(config map[string]interface{}) ([]string, error) {
	// 1. Parse Configuration
	apiID, _ := config["api_id"].(int)
	apiHash, _ := config["api_hash"].(string)

	targetLimit, _ := config["limit"].(int)
	if targetLimit == 0 {
		targetLimit = 500 // Default to 500 if not specified
	}

	sessionFile, _ := config["session_file"].(string)
	if sessionFile == "" {
		sessionFile = "telegram.session"
	}

	// Chat IDs can be mixed types in YAML
	var targetChatIDs []int64
	if chats, ok := config["chats"].([]interface{}); ok {
		for _, chat := range chats {
			if id, ok := chat.(int); ok {
				targetChatIDs = append(targetChatIDs, int64(id))
			} else if id, ok := chat.(int64); ok {
				targetChatIDs = append(targetChatIDs, id)
			}
		}
	}

	if apiID == 0 || apiHash == "" {
		return nil, fmt.Errorf("missing api_id or api_hash")
	}

	// 2. Setup Proxy (if injected)
	var dialer proxy.Dialer = proxy.Direct
	if proxyURL, ok := config["_proxy_url"].(string); ok && proxyURL != "" {
		u, err := url.Parse(proxyURL)
		if err == nil {
			d, err := proxy.FromURL(u, proxy.Direct)
			if err == nil {
				dialer = d
				logger.Log.Infof("Telegram using proxy: %s", proxyURL)
			}
		}
	}

	// 3. Initialize Client
	ctx := context.Background()
	sessionDir := filepath.Dir(sessionFile)
	if sessionDir != "." && sessionDir != "" {
		_ = os.MkdirAll(sessionDir, 0700)
	}

	clientOpts := telegram.Options{
		SessionStorage: &telegram.FileSessionStorage{Path: sessionFile},
		Resolver: dcs.Plain(dcs.PlainOptions{
			Dial: func(ctx context.Context, network, addr string) (net.Conn, error) {
				return dialer.Dial(network, addr)
			},
		}),
	}

	client := telegram.NewClient(apiID, apiHash, clientOpts)
	var allLinks []string

	// 4. Run the Client
	err := client.Run(ctx, func(ctx context.Context) error {
		// Authenticate
		flow := auth.NewFlow(
			termAuth{},
			auth.SendCodeOptions{},
		)
		if err := client.Auth().IfNecessary(ctx, flow); err != nil {
			return fmt.Errorf("authentication failed: %w", err)
		}

		logger.Log.Info("🔓 Telegram Login Successful")
		api := client.API()

		logger.Log.Info("📇 Fetching dialog list to resolve access hashes...")
		peerMap := make(map[int64]tg.InputPeerClass)

		// Get dialogs to resolve chat IDs to InputPeers
		dialogs, err := api.MessagesGetDialogs(ctx, &tg.MessagesGetDialogsRequest{
			OffsetPeer: &tg.InputPeerEmpty{},
			Limit:      100,
		})
		if err != nil {
			return fmt.Errorf("failed to get dialogs: %w", err)
		}

		var chats []tg.ChatClass
		switch d := dialogs.(type) {
		case *tg.MessagesDialogs:
			chats = d.Chats
		case *tg.MessagesDialogsSlice:
			chats = d.Chats
		}

		for _, chat := range chats {
			switch c := chat.(type) {
			case *tg.Channel:
				peerMap[c.ID] = &tg.InputPeerChannel{
					ChannelID:  c.ID,
					AccessHash: c.AccessHash,
				}
				peerMap[convertToNeg100(c.ID)] = peerMap[c.ID]
			case *tg.Chat:
				peerMap[c.ID] = &tg.InputPeerChat{
					ChatID: c.ID,
				}
				peerMap[convertToNeg(c.ID)] = peerMap[c.ID]
			}
		}

		// 5. Scrape Targets with Pagination
		for _, targetID := range targetChatIDs {
			inputPeer, found := peerMap[targetID]
			if !found {
				logger.Log.Warnf("Could not resolve chat ID %d (User not joined or not in recent dialogs)", targetID)
				continue
			}

			logger.Log.Infof("📥 Scraping Chat ID: %d (Limit: %d)...", targetID, targetLimit)

			totalFetched := 0
			offsetID := 0
			chatLinksCount := 0

			for totalFetched < targetLimit {
				// Calculate batch size (Max 100 per API spec)
				batchSize := 100
				remaining := targetLimit - totalFetched
				if remaining < 100 {
					batchSize = remaining
				}

				history, err := api.MessagesGetHistory(ctx, &tg.MessagesGetHistoryRequest{
					Peer:     inputPeer,
					Limit:    batchSize,
					OffsetID: offsetID, // Fixed casing here
				})
				if err != nil {
					logger.Log.Errorf("Failed to fetch history batch: %v", err)
					break
				}

				var messages []tg.MessageClass
				switch h := history.(type) {
				case *tg.MessagesMessages:
					messages = h.Messages
				case *tg.MessagesMessagesSlice:
					messages = h.Messages
				case *tg.MessagesChannelMessages:
					messages = h.Messages
				}

				if len(messages) == 0 {
					break // No more messages in history
				}

				for _, msg := range messages {
					if m, ok := msg.(*tg.Message); ok {
						// Extract links
						links := xray.ExtractLinks(m.Message)
						if len(links) > 0 {
							allLinks = append(allLinks, links...)
							chatLinksCount += len(links)
						}
						// Update offset logic
						if m.ID < offsetID || offsetID == 0 {
							offsetID = m.ID
						}
					}
				}
				
				// Ensure offsetID is set to the last message ID to get older messages in next loop
				if len(messages) > 0 {
					lastMsg := messages[len(messages)-1]
					if m, ok := lastMsg.(*tg.Message); ok {
						offsetID = m.ID
					}
				}

				totalFetched += len(messages)
			}
			logger.Log.Infof("    ↳ Found %d links in %d messages.", chatLinksCount, totalFetched)
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	return allLinks, nil
}

// Helpers
func convertToNeg100(id int64) int64 { return -1000000000000 - id }
func convertToNeg(id int64) int64    { return -id }

// Auth Flow
type termAuth struct{}

func (termAuth) Phone(_ context.Context) (string, error) {
	fmt.Print("📞 Enter Phone Number: ")
	reader := bufio.NewReader(os.Stdin)
	text, _ := reader.ReadString('\n')
	return strings.TrimSpace(text), nil
}

func (termAuth) Password(_ context.Context) (string, error) {
	fmt.Print("🔐 Enter 2FA Password: ")
	reader := bufio.NewReader(os.Stdin)
	text, _ := reader.ReadString('\n')
	return strings.TrimSpace(text), nil
}

func (termAuth) Code(_ context.Context, _ *tg.AuthSentCode) (string, error) {
	fmt.Print("📩 Enter Code: ")
	reader := bufio.NewReader(os.Stdin)
	text, _ := reader.ReadString('\n')
	return strings.TrimSpace(text), nil
}

func (termAuth) SignUp(_ context.Context) (auth.UserInfo, error) {
	reader := bufio.NewReader(os.Stdin)

	fmt.Print("👤 Enter First Name: ")
	firstName, _ := reader.ReadString('\n')

	fmt.Print("👤 Enter Last Name: ")
	lastName, _ := reader.ReadString('\n')

	return auth.UserInfo{
		FirstName: strings.TrimSpace(firstName),
		LastName:  strings.TrimSpace(lastName),
	}, nil
}

func (termAuth) AcceptTermsOfService(_ context.Context, tos tg.HelpTermsOfService) error {
	return nil
}

func init() {
	collectors.Register("telegram", func() collectors.Collector {
		return &TelegramCollector{}
	})
}
