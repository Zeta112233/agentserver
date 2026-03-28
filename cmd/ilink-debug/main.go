// Minimal iLink debug tool: QR login → poll → receive messages → dump & download media.
package main

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/agentserver/agentserver/internal/weixin"
)

func main() {
	// Step 1: QR Login
	log.Println("Starting iLink QR login...")
	session, err := weixin.StartLogin(context.Background(), weixin.DefaultAPIBaseURL)
	if err != nil {
		log.Fatalf("StartLogin failed: %v", err)
	}
	fmt.Printf("\n=== Scan this QR code with WeChat ===\nURL: %s\n\n", session.QRCodeURL)

	// Step 2: Poll for scan result
	var token, botID, baseURL string
	for {
		result, err := weixin.PollLoginStatus(context.Background(), weixin.DefaultAPIBaseURL, session.QRCode)
		if err != nil {
			log.Printf("PollLoginStatus error: %v", err)
			continue
		}
		log.Printf("QR status: %s", result.Status)
		if result.Status == "confirmed" {
			token = result.Token
			botID = result.BotID
			baseURL = result.BaseURL
			if baseURL == "" {
				baseURL = weixin.DefaultAPIBaseURL
			}
			log.Printf("Login success! botID=%s baseURL=%s", botID, baseURL)
			break
		}
		if result.Status == "expired" {
			log.Fatal("QR code expired, restart the tool")
		}
	}

	// Step 3: Poll for messages
	log.Println("\nWaiting for messages... Send an image via WeChat to test.")
	cursor := ""
	for {
		resp, err := weixin.GetUpdates(context.Background(), baseURL, token, cursor)
		if err != nil {
			log.Printf("GetUpdates error: %v", err)
			time.Sleep(2 * time.Second)
			continue
		}
		if resp.Ret != 0 || resp.ErrCode != 0 {
			log.Printf("GetUpdates API error: ret=%d errcode=%d errmsg=%s", resp.Ret, resp.ErrCode, resp.ErrMsg)
			time.Sleep(2 * time.Second)
			continue
		}
		if resp.GetUpdatesBuf != "" {
			cursor = resp.GetUpdatesBuf
		}

		for _, msg := range resp.Msgs {
			// Dump full message JSON
			msgJSON, _ := json.MarshalIndent(msg, "", "  ")
			fmt.Printf("\n=== Message from %s ===\n%s\n", msg.FromUserID, string(msgJSON))

			// Try to download media
			for _, item := range msg.ItemList {
				if item.Type == 2 && item.ImageItem != nil && item.ImageItem.Media != nil {
					media := item.ImageItem.Media
					fmt.Printf("\n--- Image detected ---\n")
					fmt.Printf("encrypt_query_param (%d chars): %s\n", len(media.EncryptQueryParam), media.EncryptQueryParam)
					fmt.Printf("aes_key: %s\n", media.AESKey)
					fmt.Printf("encrypt_type: %d\n", media.EncryptType)
					fmt.Printf("image_item.aeskey: %s\n", item.ImageItem.AESKey)
					fmt.Printf("full_url: %s\n", media.FullURL)
					fmt.Printf("image_item.url: %s\n", item.ImageItem.URL)
					fmt.Printf("mid_size: %d, hd_size: %d, thumb_size: %d\n", item.ImageItem.MidSize, item.ImageItem.HdSize, item.ImageItem.ThumbSize)
					if item.ImageItem.ThumbMedia != nil {
						fmt.Printf("thumb_media.encrypt_query_param (%d chars)\n", len(item.ImageItem.ThumbMedia.EncryptQueryParam))
					}

					tryDownload(media.EncryptQueryParam, media.AESKey, item.ImageItem.AESKey, media.FullURL)
				}
			}
		}
	}
}

func tryDownload(encryptQueryParam, mediaAESKey, imageAESKey, fullURL string) {
	cdnBaseURL := weixin.DefaultCDNBaseURL

	// Try 1: DownloadAndDecryptMedia with media.aes_key
	if mediaAESKey != "" {
		log.Printf("Try 1: DownloadAndDecryptMedia with media.aes_key (%d chars)...", len(mediaAESKey))
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		data, err := weixin.DownloadAndDecryptMedia(ctx, cdnBaseURL, encryptQueryParam, mediaAESKey, fullURL)
		cancel()
		if err != nil {
			log.Printf("  FAILED: %v", err)
		} else {
			saveFile(data, "media_aeskey")
			return
		}
	}

	// Try 2: With image_item.aeskey (hex → base64)
	if imageAESKey != "" {
		log.Printf("Try 2: DownloadAndDecryptMedia with image_item.aeskey hex→base64...")
		raw, err := hex.DecodeString(imageAESKey)
		if err == nil {
			b64 := base64.StdEncoding.EncodeToString(raw)
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			data, err := weixin.DownloadAndDecryptMedia(ctx, cdnBaseURL, encryptQueryParam, b64, fullURL)
			cancel()
			if err != nil {
				log.Printf("  FAILED: %v", err)
			} else {
				saveFile(data, "image_aeskey_hex")
				return
			}
		}
	}

	// Try 3: Plain download (no decryption)
	log.Printf("Try 3: Plain download (no decryption)...")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	data, err := weixin.DownloadFromCDN(ctx, cdnBaseURL, encryptQueryParam, fullURL)
	cancel()
	if err != nil {
		log.Printf("  FAILED: %v", err)
	} else {
		saveFile(data, "plain")
	}
}

func saveFile(data []byte, label string) {
	filename := fmt.Sprintf("/tmp/weixin-image-%s-%d.bin", label, time.Now().Unix())
	if err := os.WriteFile(filename, data, 0644); err != nil {
		log.Printf("  Failed to save: %v", err)
		return
	}
	log.Printf("  SUCCESS! Saved %d bytes to %s", len(data), filename)
}
