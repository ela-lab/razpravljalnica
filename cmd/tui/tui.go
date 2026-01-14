package main

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strconv"
	"time"

	"github.com/ela-lab/razpravljalnica/api"
	"github.com/ela-lab/razpravljalnica/internal/client"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

type TUIApp struct {
	app         *tview.Application
	headService *client.ClientService
	tailService *client.ClientService
	cpClient    api.ControlPlaneClient
	cpConn      *grpc.ClientConn
	cpURL       string
	headAddr    string
	tailAddr    string
	currentUser int64
	mainLayout  tview.Primitive

	// UI components
	topicList   *tview.List
	messageView *tview.Table
	inputField  *tview.InputField
	statusBar   *tview.TextView
	topBar      *tview.TextView
	rightPanel  tview.Primitive

	currentTopicID       int64
	focusOnInput         bool
	messages             []*api.Message   // Cache messages to support liking
	userNames            map[int64]string // Cache user names for display
	currentUserName      string           // Name of the currently logged-in user
	selectedMessageIndex int
	topics               []*api.Topic       // Cache topics list
	subscribedTopics     map[int64]bool     // Track subscribed topics
	autoScroll           bool               // Auto-scroll to bottom when new messages arrive
	likedMessages        map[int64]bool     // Track which messages current user has liked
	subCancel            context.CancelFunc // Cancel active subscription stream
	editingMessageID     int64              // Message ID being edited (0 if not editing)
	editingMessageIndex  int                // Index of message being edited
	inDialog             bool               // True when a modal form is active
}

// fetchClusterAddresses returns head/tail addresses from control plane.
func fetchClusterAddresses(cp api.ControlPlaneClient) (string, string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	resp, err := cp.GetClusterState(ctx, &emptypb.Empty{})
	if err != nil {
		return "", "", err
	}

	var headAddr, tailAddr string
	if resp.GetHead() != nil {
		headAddr = resp.GetHead().GetAddress()
	}
	if resp.GetTail() != nil {
		tailAddr = resp.GetTail().GetAddress()
	}
	return headAddr, tailAddr, nil
}

// updateServices reconnects head/tail clients if addresses change.
func (t *TUIApp) updateServices(headAddr, tailAddr string) error {
	if headAddr == "" {
		return fmt.Errorf("head address is empty")
	}
	if tailAddr == "" {
		tailAddr = headAddr
	}
	// No change and clients already exist
	if headAddr == t.headAddr && tailAddr == t.tailAddr && t.headService != nil && t.tailService != nil {
		return nil
	}

	// Create new clients first
	newHead, err := client.NewClientService(headAddr, 10*time.Second)
	if err != nil {
		return fmt.Errorf("failed to connect to head %s: %w", headAddr, err)
	}
	newTail, err := client.NewClientService(tailAddr, 10*time.Second)
	if err != nil {
		newHead.Close()
		return fmt.Errorf("failed to connect to tail %s: %w", tailAddr, err)
	}

	// Swap
	oldHead := t.headService
	oldTail := t.tailService
	t.headService = newHead
	t.tailService = newTail
	t.headAddr = headAddr
	t.tailAddr = tailAddr

	if oldHead != nil {
		_ = oldHead.Close()
	}
	if oldTail != nil && oldTail != oldHead {
		_ = oldTail.Close()
	}

	return nil
}

// startClusterWatcher polls control plane for head/tail changes and reconnects automatically.
func (t *TUIApp) startClusterWatcher() {
	if t.cpClient == nil {
		return
	}

	go func() {
		ticker := time.NewTicker(5 * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			head, tail, err := fetchClusterAddresses(t.cpClient)
			if err != nil {
				log.Printf("control plane check failed: %v", err)
				continue
			}
			if head == "" {
				continue
			}
			if tail == "" {
				tail = head
			}
			if head == t.headAddr && tail == t.tailAddr {
				continue
			}

			hAddr := head
			tAddr := tail
			t.app.QueueUpdateDraw(func() {
				if err := t.updateServices(hAddr, tAddr); err != nil {
					t.showStatus(fmt.Sprintf("[red]Failed to switch head/tail: %v[white]", err))
					return
				}

				t.userNames = make(map[int64]string) // reset cache to avoid stale lookups on new tail
				t.showStatus(fmt.Sprintf("[yellow]Topology changed: head %s | tail %s[white]", hAddr, tAddr))
				// Refresh UI data and restart streaming with new endpoints
				_ = t.loadTopics()
				if t.currentTopicID == 0 {
					t.loadSubscriptionFeed()
				} else {
					t.loadMessages(t.currentTopicID)
				}
				t.restartSubscriptionForCurrentView()
			})
		}
	}()
}

func RunTUI(headURL, tailURL, controlPlaneURL string) error {
	// Connect to control plane first (best effort)
	var cpConn *grpc.ClientConn
	var cpClient api.ControlPlaneClient
	if controlPlaneURL != "" {
		fmt.Printf("Connecting to control plane %s...\n", controlPlaneURL)
		conn, err := grpc.NewClient(controlPlaneURL, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err == nil {
			cpConn = conn
			cpClient = api.NewControlPlaneClient(conn)
		} else {
			fmt.Printf("Warning: failed to connect to control plane (%v). Using provided head/tail.\n", err)
		}
	}
	// Discover head/tail via control plane if available
	resolvedHead := headURL
	resolvedTail := tailURL
	if cpClient != nil {
		h, t, err := fetchClusterAddresses(cpClient)
		if err == nil {
			if h != "" {
				resolvedHead = h
			}
			if resolvedTail == "" && t != "" {
				resolvedTail = t
			}
		}
	}
	if resolvedTail == "" {
		resolvedTail = resolvedHead
	}

	fmt.Printf("Using head %s and tail %s\n", resolvedHead, resolvedTail)

	headService, err := client.NewClientService(resolvedHead, 10*time.Second)
	if err != nil {
		return fmt.Errorf("failed to connect to head: %w", err)
	}

	tailService, err := client.NewClientService(resolvedTail, 10*time.Second)
	if err != nil {
		headService.Close()
		return fmt.Errorf("failed to connect to tail: %w", err)
	}

	tui := &TUIApp{
		app:              tview.NewApplication(),
		headService:      headService,
		tailService:      tailService,
		cpClient:         cpClient,
		cpConn:           cpConn,
		cpURL:            controlPlaneURL,
		headAddr:         resolvedHead,
		tailAddr:         resolvedTail,
		userNames:        make(map[int64]string),
		subscribedTopics: make(map[int64]bool),
		likedMessages:    make(map[int64]bool),
		autoScroll:       true,
	}
	defer tui.closeServices()

	// Create UI
	if err := tui.createUI(); err != nil {
		return err
	}

	// Start monitoring cluster topology to react to head/tail changes
	tui.startClusterWatcher()

	// Run application
	if err := tui.app.Run(); err != nil {
		return err
	}

	return nil
}

func (t *TUIApp) createUI() error {
	// Topic list (left panel)
	t.topicList = tview.NewList().ShowSecondaryText(false)
	t.topicList.SetBorder(true).SetTitle("Topics")
	t.topicList.SetSelectedFunc(t.onTopicSelected)

	// Message view (top right) - Table to allow multiline rows
	t.messageView = tview.NewTable().SetSelectable(true, false)
	t.messageView.SetBorder(true).SetTitle("Messages")
	// Selection change just tracks index
	t.messageView.SetSelectionChangedFunc(func(row, column int) {
		t.selectedMessageIndex = row
	})
	// Enter on a row triggers like toggle
	t.messageView.SetSelectedFunc(func(row, column int) {
		t.selectedMessageIndex = row
		t.likeMessage()
	})

	// Detect when user navigates messages to break auto-scroll
	t.messageView.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyUp || event.Key() == tcell.KeyDown ||
			event.Key() == tcell.KeyPgUp || event.Key() == tcell.KeyPgDn {
			t.autoScroll = false
		} else if event.Key() == tcell.KeyEnd {
			t.autoScroll = true
		} else if event.Key() == tcell.KeyEsc {
			// Let ESC propagate to global handler for unfocusing
			return event
		}
		return event
	})

	// Input field (bottom right)
	t.inputField = tview.NewInputField().
		SetLabel("Message: ").
		SetFieldWidth(0)
	t.inputField.SetBorder(true).SetTitle("Send Message")
	t.inputField.SetDoneFunc(t.onMessageSend)
	// Handle ESC to exit edit mode
	t.inputField.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEsc && t.editingMessageID != 0 {
			t.exitEditMode()
			return nil
		}
		return event
	})

	// Status bar
	t.statusBar = tview.NewTextView().
		SetDynamicColors(true).
		SetText("[yellow]Press F1 for help | F2: Login | F3: New Topic | F12: Quit[white]")
	t.statusBar.SetBorder(false)

	// Top bar (user info, time, subscriptions)
	t.topBar = tview.NewTextView().
		SetDynamicColors(true).
		SetTextAlign(tview.AlignLeft).
		SetWrap(false)
	t.topBar.SetBorder(false)

	// Right panel layout
	rightPanel := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(t.messageView, 0, 1, false).
		AddItem(t.inputField, 3, 0, true).
		AddItem(t.statusBar, 1, 0, false)

	// Main layout (content area)
	mainLayout := tview.NewFlex().
		AddItem(t.topicList, 0, 1, true).
		AddItem(rightPanel, 0, 3, false)

	// Root layout with top bar
	rootLayout := tview.NewFlex().SetDirection(tview.FlexRow).
		AddItem(t.topBar, 1, 0, false).
		AddItem(mainLayout, 0, 1, true)

	t.rightPanel = rightPanel

	// Store root layout (used when closing dialogs)
	t.mainLayout = rootLayout

	// Load initial data
	if err := t.loadTopics(); err != nil {
		return err
	}

	// Periodically refresh topics so new ones appear for all users
	t.startTopicsTicker()

	// Initial top bar info and ticker
	t.refreshTopBar()
	t.startTopBarTicker()

	// Set up keyboard shortcuts
	t.app.SetInputCapture(t.handleGlobalKeys)

	t.app.SetRoot(rootLayout, true)
	return nil
}

func (t *TUIApp) handleGlobalKeys(event *tcell.EventKey) *tcell.EventKey {
	// Ignore global shortcuts while a modal dialog is active
	if t.inDialog {
		return event
	}
	// Handle Tab key for focus switching between topicList -> messageView -> inputField
	if event.Key() == tcell.KeyTab {
		current := t.app.GetFocus()
		switch current {
		case t.topicList:
			t.app.SetFocus(t.messageView)
			t.focusOnInput = false
			t.showStatus("[green]Focus on messages[white]")
		case t.messageView:
			t.app.SetFocus(t.inputField)
			t.focusOnInput = true
			t.showStatus("[green]Focus on input field[white]")
		default: // inputField or anything else
			t.app.SetFocus(t.topicList)
			t.focusOnInput = false
			t.showStatus("[green]Focus on topic list[white]")
		}
		return nil
	}

	switch event.Key() {
	case tcell.KeyEsc:
		// ESC unfocuses - goes back to topic list
		t.app.SetFocus(t.topicList)
		t.focusOnInput = false
		t.showStatus("[green]Focus on topic list[white]")
		return nil
	case tcell.KeyF12:
		t.app.Stop()
		return nil
	case tcell.KeyF1:
		t.showHelp()
		return nil
	case tcell.KeyF2:
		t.showLoginDialog()
		return nil
	case tcell.KeyF3:
		if t.currentUser != 0 {
			t.showNewTopicDialog()
		} else {
			t.showStatus("[red]Please login first (F2)[white]")
		}
		return nil
	}

	// Handle 'S' key to toggle subscription on selected topic
	if !t.focusOnInput && (event.Rune() == 's' || event.Rune() == 'S') {
		selectedTopicIndex := t.topicList.GetCurrentItem()
		if selectedTopicIndex > 0 && selectedTopicIndex <= len(t.topics) {
			t.toggleSubscription(selectedTopicIndex - 1)
		}
		return nil
	}

	// Handle 'E' key to edit selected message
	if !t.focusOnInput && (event.Rune() == 'e' || event.Rune() == 'E') {
		if t.currentUser != 0 && t.selectedMessageIndex >= 0 && t.selectedMessageIndex < len(t.messages) {
			msg := t.messages[t.selectedMessageIndex]
			// Check if user owns the message
			if msg.UserId == t.currentUser {
				t.enterEditMode(msg)
			} else {
				t.showStatus("[red]You can only edit your own messages[white]")
			}
		}
		return nil
	}

	// Handle Delete key to delete selected message
	if !t.focusOnInput && event.Key() == tcell.KeyDelete {
		t.deleteMessage()
		return nil
	}

	return event
}

func (t *TUIApp) loadTopics() error {
	topics, err := t.tailService.ListTopics()
	if err != nil {
		return err
	}

	// Keep topics ordered by ID for stable UI
	sort.Slice(topics, func(i, j int) bool {
		return topics[i].Id < topics[j].Id
	})

	t.topics = topics
	t.topicList.Clear()

	// Add subscription feed as first item
	t.topicList.AddItem("📬 Subscription Feed", "", 0, nil)

	for _, topic := range topics {
		topicName := topic.Name
		// Add indicator if subscribed
		if t.subscribedTopics[topic.Id] {
			topicName = "✓ " + topicName
		}
		t.topicList.AddItem(topicName, "", 0, nil)
	}

	return nil
}

// startTopicsTicker refreshes topics periodically so new topics appear without manual reload
func (t *TUIApp) startTopicsTicker() {
	go func() {
		ticker := time.NewTicker(5 * time.Second)
		for range ticker.C {
			t.app.QueueUpdateDraw(func() {
				current := t.topicList.GetCurrentItem()
				if err := t.loadTopics(); err == nil {
					count := t.topicList.GetItemCount()
					if count > 0 {
						if current >= count {
							current = count - 1
						}
						t.topicList.SetCurrentItem(current)
					}
				}
			})
		}
	}()
}

func (t *TUIApp) onTopicSelected(index int, mainText, secondaryText string, shortcut rune) {
	// Index 0 is subscription feed
	if index == 0 {
		t.currentTopicID = 0 // Special ID for subscription feed
		t.loadSubscriptionFeed()
		// subscribe to all subscribed topics
		var ids []int64
		for id, sub := range t.subscribedTopics {
			if sub {
				ids = append(ids, id)
			}
		}
		t.startSubscription(context.Background(), ids)
		t.app.SetFocus(t.messageView)
		t.focusOnInput = false
		return
	}

	// Regular topics start at index 1
	topicIndex := index - 1
	if topicIndex >= len(t.topics) {
		return
	}

	t.currentTopicID = t.topics[topicIndex].Id
	t.loadMessages(t.currentTopicID)
	// subscribe to this topic only
	t.startSubscription(context.Background(), []int64{t.currentTopicID})
	// Shift focus to messages after selecting a topic
	t.app.SetFocus(t.messageView)
	t.focusOnInput = false
}

const (
	messageFetchLimit  = 5000 // large window to pull many messages
	messageDisplayKeep = 1000 // keep newest N in the UI
)

func (t *TUIApp) loadMessages(topicID int64) {
	messages, err := t.tailService.GetMessages(topicID, 0, messageFetchLimit)
	if err != nil {
		t.showStatus(fmt.Sprintf("[red]Error loading messages: %v[white]", err))
		return
	}

	if len(messages) > messageDisplayKeep {
		messages = messages[len(messages)-messageDisplayKeep:]
	}

	// Preserve current selection
	prevSelectedRow, _ := t.messageView.GetSelection()
	if prevSelectedRow < 0 {
		prevSelectedRow = 0
	}

	t.messages = messages
	t.messageView.Clear()

	row := 0
	for _, msg := range messages {
		timestamp := msg.CreatedAt.AsTime().Format("15:04:05")
		username := t.ensureUserName(msg.UserId)

		baseText := fmt.Sprintf("[%s] %s: %s", timestamp, username, msg.Text)
		wrapWidth := t.getWrapWidth()
		wrapped := wrapText(baseText, wrapWidth)

		textCell := tview.NewTableCell(wrapped).
			SetMaxWidth(wrapWidth).
			SetExpansion(1)
		likeText := ""
		if msg.Likes > 0 {
			likeText = fmt.Sprintf("👍 %d", msg.Likes)
		}
		likeCell := tview.NewTableCell(likeText).
			SetAlign(tview.AlignRight).
			SetMaxWidth(8).
			SetExpansion(0)

		t.messageView.SetCell(row, 0, textCell)
		t.messageView.SetCell(row, 1, likeCell)
		row++
	}

	// Restore selection or go to bottom if auto-scroll
	if t.autoScroll && len(messages) > 0 {
		t.messageView.Select(len(messages)-1, 0)
	} else if prevSelectedRow < len(messages) {
		t.messageView.Select(prevSelectedRow, 0)
	}
}

func (t *TUIApp) onMessageSend(key tcell.Key) {
	if key != tcell.KeyEnter {
		return
	}

	text := t.inputField.GetText()
	if text == "" {
		return
	}

	// If editing a message, update it instead of posting a new one
	if t.editingMessageID != 0 {
		_, err := t.headService.UpdateMessage(t.currentUser, t.currentTopicID, t.editingMessageID, text)
		if err != nil {
			t.showStatus(fmt.Sprintf("[red]Failed to update message: %v[white]", err))
		} else {
			t.showStatus("[green]Message updated![white]")
			// Reload appropriate view
			if t.currentTopicID == 0 {
				t.loadSubscriptionFeed()
			} else {
				t.loadMessages(t.currentTopicID)
			}
			// Restore selection
			t.messageView.Select(t.editingMessageIndex, 0)
		}

		// Exit edit mode
		t.exitEditMode()
		return
	}

	// Regular message sending
	if t.currentTopicID == 0 || t.currentUser == 0 {
		if t.currentUser == 0 {
			t.showStatus("[red]Please login first (F2)[white]")
		}
		return
	}

	_, err := t.headService.PostMessage(t.currentUser, t.currentTopicID, text)
	if err != nil {
		t.showStatus(fmt.Sprintf("[red]Error: %v[white]", err))
		return
	}

	t.inputField.SetText("")
	t.loadMessages(t.currentTopicID)
	t.showStatus("[green]Message sent![white]")
}

func (t *TUIApp) showLoginDialog() {
	form := tview.NewForm()
	form.AddInputField("User ID:", "", 20, nil, nil)
	form.AddInputField("Register name:", "", 30, nil, nil)

	form.AddButton("Login by ID", func() {
		idText := form.GetFormItem(0).(*tview.InputField).GetText()
		if idText == "" {
			form.SetTitle("[red]User ID is required[white]")
			return
		}
		id, err := strconv.ParseInt(idText, 10, 64)
		if err != nil || id <= 0 {
			form.SetTitle("[red]User ID must be a positive number[white]")
			return
		}

		user, err := t.tailService.GetUser(id)
		if err != nil {
			form.SetTitle(fmt.Sprintf("[red]Login failed: %v[white]", err))
			return
		}

		t.currentUser = user.Id
		t.currentUserName = user.Name
		t.userNames[user.Id] = user.Name
		t.showStatus(fmt.Sprintf("[green]Logged in as %s (ID: %d)[white]", user.Name, user.Id))
		// Reload subscriptions from server so they persist across sessions
		t.syncSubscriptionsFromServer()
		t.restartSubscriptionForCurrentView()
		t.inDialog = false
		t.app.SetRoot(t.mainLayout, true)
	})

	form.AddButton("Register New", func() {
		name := form.GetFormItem(1).(*tview.InputField).GetText()
		if name == "" {
			form.SetTitle("[red]Name is required to register[white]")
			return
		}

		user, err := t.headService.CreateUser(name)
		if err != nil {
			t.showStatus(fmt.Sprintf("[red]Register failed: %v[white]", err))
			return
		}

		t.currentUser = user.Id
		t.currentUserName = user.Name
		t.userNames[user.Id] = user.Name
		t.showStatus(fmt.Sprintf("[green]Registered as %s (ID: %d)[white]", user.Name, user.Id))
		// New users start with empty subscriptions; sync for consistency
		t.syncSubscriptionsFromServer()
		t.restartSubscriptionForCurrentView()
		t.inDialog = false
		t.app.SetRoot(t.mainLayout, true)
	})

	form.AddButton("Cancel", func() {
		t.inDialog = false
		t.app.SetRoot(t.mainLayout, true)
	})

	// Prevent keys from reaching global handler
	form.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEsc {
			t.inDialog = false
			t.app.SetRoot(t.mainLayout, true)
			return nil
		}
		// Consume function keys and let form handle Tab internally
		if event.Key() >= tcell.KeyF1 && event.Key() <= tcell.KeyF12 {
			return nil
		}
		return event
	})

	form.SetBorder(true).SetTitle("Login")
	t.inDialog = true
	t.app.SetRoot(form, true)
}

// restartSubscriptionForCurrentView restarts streaming for the currently viewed topic/feed
func (t *TUIApp) restartSubscriptionForCurrentView() {
	if t.currentUser == 0 {
		return
	}

	if t.currentTopicID == 0 {
		var ids []int64
		for id, sub := range t.subscribedTopics {
			if sub {
				ids = append(ids, id)
			}
		}
		t.startSubscription(context.Background(), ids)
		return
	}

	// If a topic is selected
	t.startSubscription(context.Background(), []int64{t.currentTopicID})
}

func (t *TUIApp) showNewTopicDialog() {
	form := tview.NewForm()
	form.AddInputField("Topic Name:", "", 30, nil, nil)
	form.AddButton("Create", func() {
		topicName := form.GetFormItem(0).(*tview.InputField).GetText()
		if topicName == "" {
			return
		}

		topic, err := t.headService.CreateTopic(topicName)
		if err != nil {
			t.showStatus(fmt.Sprintf("[red]Error: %v[white]", err))
			t.inDialog = false
			t.app.SetRoot(t.mainLayout, true)
			return
		}

		t.showStatus(fmt.Sprintf("[green]Topic '%s' created (ID: %d)[white]", topic.Name, topic.Id))
		t.loadTopics()
		t.inDialog = false
		t.app.SetRoot(t.mainLayout, true)
	})
	form.AddButton("Cancel", func() {
		t.inDialog = false
		t.app.SetRoot(t.mainLayout, true)
	})

	// Prevent keys from reaching global handler
	form.SetInputCapture(func(event *tcell.EventKey) *tcell.EventKey {
		if event.Key() == tcell.KeyEsc {
			t.inDialog = false
			t.app.SetRoot(t.mainLayout, true)
			return nil
		}
		// Consume function keys and let form handle Tab internally
		if event.Key() >= tcell.KeyF1 && event.Key() <= tcell.KeyF12 {
			return nil
		}
		return event
	})

	form.SetBorder(true).SetTitle("New Topic")
	t.inDialog = true
	t.app.SetRoot(form, true)
}

func (t *TUIApp) enterEditMode(msg *api.Message) {
	t.editingMessageID = msg.Id
	t.editingMessageIndex = t.selectedMessageIndex
	t.inputField.SetLabel("Edit: ")
	t.inputField.SetText(msg.Text)
	t.inputField.SetTitle("Edit Message (Press Enter to save, Esc to cancel)")
	t.app.SetFocus(t.inputField)
	t.focusOnInput = true
	t.showStatus("[green]Editing message - Press Enter to save or Esc to cancel[white]")
}

func (t *TUIApp) exitEditMode() {
	t.editingMessageID = 0
	t.editingMessageIndex = -1
	t.inputField.SetLabel("Message: ")
	t.inputField.SetText("")
	t.inputField.SetTitle("Send Message")
	t.focusOnInput = false
	t.app.SetFocus(t.messageView)
}

func (t *TUIApp) showHelp() {
	helpText := `[yellow]Keyboard Shortcuts:[white]

F1      - Show this help
F2      - Login by User ID / Register new
F3      - Create New Topic
S       - Subscribe/Unsubscribe to selected topic
E       - Edit selected message (if you own it)
Del     - Delete selected message (if you own it)
Enter   - Select topic / Send message / Like/Unlike message
ESC     - Unfocus (return to topic list)
F12     - Quit application

[yellow]Navigation:[white]
Arrow keys  - Navigate lists (breaks auto-scroll in messages)
Tab         - Switch focus between panels
End         - Re-enable auto-scroll in messages

[yellow]Usage:[white]
1. Press F2 to login or register a new user
2. Press F3 to create a new topic
3. Select a topic from the list (✓ indicates subscribed)
4. Navigate to a message and press Enter to like/unlike it
5. Press E on your own message to edit it
6. Type your message and press Enter to send
7. Subscription Feed (first topic) shows messages from all subscribed topics
`

	textView := tview.NewTextView().
		SetDynamicColors(true).
		SetText(helpText)
	textView.SetBorder(true).SetTitle("Help")

	textView.SetDoneFunc(func(key tcell.Key) {
		t.app.SetRoot(t.mainLayout, true)
	})

	t.app.SetRoot(textView, true)
}

func (t *TUIApp) showStatus(message string) {
	t.statusBar.SetText(message)
}

func (t *TUIApp) startSubscription(ctx context.Context, topicIDs []int64) {
	if t.currentUser == 0 {
		return
	}

	if len(topicIDs) == 0 {
		return
	}

	// cancel previous stream if any
	if t.subCancel != nil {
		t.subCancel()
	}

	ctx, cancel := context.WithCancel(ctx)
	t.subCancel = cancel

	// Build subscriptions list - get token and node for each topic
	subscriptions := make([]struct {
		TopicID   int64
		Token     string
		FromMsgID int64
		NodeAddr  string
	}, len(topicIDs))

	for i, topicID := range topicIDs {
		token, node, err := t.headService.GetSubscriptionNode(t.currentUser, topicID)
		if err != nil {
			t.showStatus(fmt.Sprintf("[red]Subscription error for topic %d: %v[white]", topicID, err))
			return
		}
		subscriptions[i] = struct {
			TopicID   int64
			Token     string
			FromMsgID int64
			NodeAddr  string
		}{
			TopicID:   topicID,
			Token:     token,
			FromMsgID: 0,
			NodeAddr:  node.Address,
		}
	}

	go func() {
		err := t.headService.StreamMultipleSubscriptions(ctx, t.currentUser, subscriptions, func(event *api.MessageEvent) error {
			// Resolve sender name if unknown
			t.ensureUserName(event.Message.UserId)

			// Refresh current view based on what is open
			if t.currentTopicID == 0 {
				t.app.QueueUpdateDraw(func() {
					t.loadSubscriptionFeed()
				})
			} else if event.Message.TopicId == t.currentTopicID {
				t.app.QueueUpdateDraw(func() {
					t.loadMessages(t.currentTopicID)
				})
			}
			return nil
		})
		if err != nil {
			if err == context.Canceled || status.Code(err) == codes.Canceled {
				return
			}
			t.app.QueueUpdateDraw(func() {
				t.showStatus(fmt.Sprintf("[red]Stream error: %v[white]", err))
			})
		}
	}()
}

func (t *TUIApp) onMessageSelected(index int, mainText string, secondaryText string, shortcut rune) {
	t.selectedMessageIndex = index
	// Toggle like when Enter is pressed on a message
	if t.currentUser != 0 && t.currentTopicID != 0 {
		t.likeMessage()
	}
}

func (t *TUIApp) likeMessage() {
	if t.currentUser == 0 {
		t.showStatus("[red]Please login first (F2)[white]")
		return
	}

	if t.selectedMessageIndex < 0 || t.selectedMessageIndex >= len(t.messages) {
		t.showStatus("[red]No message selected[white]")
		return
	}

	msg := t.messages[t.selectedMessageIndex]

	// Store current selection
	currentSelection := t.selectedMessageIndex

	// Use the message's topic ID (needed for subscription feed)
	topicID := msg.TopicId

	// Toggle like on the server (server will add/remove)
	_, err := t.headService.LikeMessage(t.currentUser, topicID, msg.Id)
	if err != nil {
		t.showStatus(fmt.Sprintf("[red]Error liking message: %v[white]", err))
		return
	}

	// Reload appropriate view and restore selection
	if t.currentTopicID == 0 {
		t.loadSubscriptionFeed()
	} else {
		t.loadMessages(t.currentTopicID)
	}
	t.messageView.Select(currentSelection, 0)
	t.showStatus("[green]Message liked/unliked![white]")
}

func (t *TUIApp) deleteMessage() {
	if t.currentUser == 0 {
		t.showStatus("[red]Please login first (F2)[white]")
		return
	}
	if t.selectedMessageIndex < 0 || t.selectedMessageIndex >= len(t.messages) {
		t.showStatus("[red]No message selected[white]")
		return
	}

	msg := t.messages[t.selectedMessageIndex]
	if msg.UserId != t.currentUser {
		t.showStatus("[red]You can only delete your own messages[white]")
		return
	}

	// Exit edit mode if deleting the message being edited
	if t.editingMessageID == msg.Id {
		t.exitEditMode()
	}

	topicID := msg.TopicId
	err := t.headService.DeleteMessage(t.currentUser, topicID, msg.Id)
	if err != nil {
		t.showStatus(fmt.Sprintf("[red]Error deleting message: %v[white]", err))
		return
	}

	// Reload view and adjust selection
	if t.currentTopicID == 0 {
		t.loadSubscriptionFeed()
	} else {
		t.loadMessages(t.currentTopicID)
	}

	// Clamp selection after deletion
	newIndex := t.selectedMessageIndex
	if newIndex >= len(t.messages) {
		newIndex = len(t.messages) - 1
	}
	if newIndex >= 0 {
		t.messageView.Select(newIndex, 0)
	}

	t.showStatus("[green]Message deleted[white]")
}

func (t *TUIApp) loadSubscriptionFeed() {
	// Get all subscribed topic IDs
	var subscribedIDs []int64
	for topicID, subscribed := range t.subscribedTopics {
		if subscribed {
			subscribedIDs = append(subscribedIDs, topicID)
		}
	}

	if len(subscribedIDs) == 0 {
		t.messageView.Clear()
		cell := tview.NewTableCell("[yellow]No subscribed topics. Press S on a topic to subscribe.[white]").
			SetExpansion(1)
		t.messageView.SetCell(0, 0, cell)
		return
	}

	// Collect messages from all subscribed topics and sort by timestamp
	t.messages = nil
	t.messageView.Clear()

	type feedEntry struct {
		msg       *api.Message
		topicName string
	}

	var all []*feedEntry
	for _, topicID := range subscribedIDs {
		messages, err := t.tailService.GetMessages(topicID, 0, messageFetchLimit)
		if err != nil {
			continue
		}
		if len(messages) > messageDisplayKeep {
			messages = messages[len(messages)-messageDisplayKeep:]
		}

		// Find topic name
		var topicName string
		for _, topic := range t.topics {
			if topic.Id == topicID {
				topicName = topic.Name
				break
			}
		}

		for _, msg := range messages {
			all = append(all, &feedEntry{msg: msg, topicName: topicName})
		}
	}

	sort.Slice(all, func(i, j int) bool {
		return all[i].msg.CreatedAt.AsTime().Before(all[j].msg.CreatedAt.AsTime())
	})

	row := 0
	wrapWidth := t.getWrapWidth()
	for _, entry := range all {
		msg := entry.msg
		timestamp := msg.CreatedAt.AsTime().Format("15:04:05")
		username := t.ensureUserName(msg.UserId)

		baseText := fmt.Sprintf("[yellow](%s)[white] [%s] %s: %s", entry.topicName, timestamp, username, msg.Text)
		wrapped := wrapText(baseText, wrapWidth)
		likeText := ""
		if msg.Likes > 0 {
			likeText = fmt.Sprintf("👍 %d", msg.Likes)
		}

		textCell := tview.NewTableCell(wrapped).
			SetMaxWidth(wrapWidth).
			SetExpansion(1)
		likeCell := tview.NewTableCell(likeText).
			SetAlign(tview.AlignRight).
			SetMaxWidth(8).
			SetExpansion(0)
		t.messageView.SetCell(row, 0, textCell)
		t.messageView.SetCell(row, 1, likeCell)
		t.messages = append(t.messages, msg)
		row++
	}

	// Auto-scroll to bottom
	if len(t.messages) > 0 {
		t.messageView.Select(len(t.messages)-1, 0)
	}
}

// refreshTopBar updates the top info bar with user, time, and subscription count.
func (t *TUIApp) refreshTopBar() {
	subscribed := 0
	for _, sub := range t.subscribedTopics {
		if sub {
			subscribed++
		}
	}

	userLabel := "Not logged in"
	if t.currentUser != 0 {
		name := t.currentUserName
		if name == "" {
			name = fmt.Sprintf("User%d", t.currentUser)
		}
		userLabel = fmt.Sprintf("User: %s (ID: %d)", name, t.currentUser)
	}

	clock := time.Now().Format("2006-01-02 15:04:05")
	text := fmt.Sprintf("[yellow]%s[white] | [cyan]%s[white] | Subscribed: [green]%d[white]", userLabel, clock, subscribed)
	t.topBar.SetText(text)
}

// syncSubscriptionsFromServer loads persisted subscriptions for the current user and updates UI state
func (t *TUIApp) syncSubscriptionsFromServer() {
	if t.currentUser == 0 {
		return
	}

	subscribedIDs, err := t.tailService.ListSubscriptions(t.currentUser)
	if err != nil {
		t.showStatus(fmt.Sprintf("[red]Failed to load subscriptions: %v[white]", err))
		return
	}

	// Replace local state with server state
	t.subscribedTopics = make(map[int64]bool)
	for _, id := range subscribedIDs {
		t.subscribedTopics[id] = true
	}

	// Refresh UI elements that depend on subscriptions
	if err := t.loadTopics(); err == nil {
		if t.currentTopicID == 0 {
			t.loadSubscriptionFeed()
		}
	}
	t.refreshTopBar()
}

// ensureUserName resolves and caches a user name by ID
func (t *TUIApp) ensureUserName(userID int64) string {
	if name, ok := t.userNames[userID]; ok && name != "" {
		return name
	}

	user, err := t.tailService.GetUser(userID)
	if err == nil && user != nil {
		t.userNames[userID] = user.Name
		return user.Name
	}

	// If not found, keep fallback but do not overwrite an existing real name later
	// (this cache entry can be replaced on future successful fetches)
	fallback := fmt.Sprintf("User%d", userID)
	t.userNames[userID] = fallback
	return fallback
}

// startTopBarTicker periodically refreshes the top bar clock.
func (t *TUIApp) startTopBarTicker() {
	go func() {
		ticker := time.NewTicker(time.Second)
		for range ticker.C {
			t.app.QueueUpdateDraw(func() {
				t.refreshTopBar()
			})
		}
	}()
}

// getWrapWidth computes a wrap width for messages based on the current table size.
// It reserves space for the like column and keeps a reasonable minimum width.
func (t *TUIApp) getWrapWidth() int {
	width := 0
	if view, ok := t.rightPanel.(interface{ GetInnerRect() (int, int, int, int) }); ok {
		_, _, width, _ = view.GetInnerRect()
	}

	if width <= 0 {
		_, _, width, _ = t.messageView.GetInnerRect()
	}

	if width <= 0 {
		width = 80
	}

	// Reserve ~10 chars for likes and spacing.
	wrapWidth := width - 10
	if wrapWidth < 20 {
		wrapWidth = 20
	}
	return wrapWidth
}

// closeServices cleans up active gRPC connections.
func (t *TUIApp) closeServices() {
	if t.headService != nil {
		_ = t.headService.Close()
	}
	if t.tailService != nil && t.tailService != t.headService {
		_ = t.tailService.Close()
	}
	if t.cpConn != nil {
		_ = t.cpConn.Close()
	}
}

// wrapText performs a simple word wrap at the given width.
// Color tags (e.g., [yellow]) do not count toward visual width.
func wrapText(s string, width int) string {
	if width <= 0 {
		return s
	}

	var out []rune
	lineLen := 0
	inTag := false
	runes := []rune(s)
	for _, r := range runes {
		// Track color tags to avoid counting them toward width
		if r == '[' {
			inTag = true
		}

		if r == '\n' {
			out = append(out, r)
			lineLen = 0
			inTag = false
			continue
		}

		out = append(out, r)

		if !inTag {
			lineLen++
			if lineLen >= width {
				out = append(out, '\n')
				lineLen = 0
			}
		}

		if r == ']' {
			inTag = false
		}
	}

	return string(out)
}

func (t *TUIApp) toggleSubscription(topicIndex int) {
	if topicIndex < 0 || topicIndex >= len(t.topics) {
		return
	}

	topic := t.topics[topicIndex]

	// Toggle subscription state
	t.subscribedTopics[topic.Id] = !t.subscribedTopics[topic.Id]

	// Reload topics to update visual indicator
	t.loadTopics()

	// Restore selection to the same topic (account for subscription feed at index 0)
	t.topicList.SetCurrentItem(topicIndex + 1)

	if t.subscribedTopics[topic.Id] {
		t.showStatus(fmt.Sprintf("[green]Subscribed to '%s'[white]", topic.Name))
	} else {
		t.showStatus(fmt.Sprintf("[yellow]Unsubscribed from '%s'[white]", topic.Name))
	}

	// Update top bar subscription count
	t.refreshTopBar()
}
