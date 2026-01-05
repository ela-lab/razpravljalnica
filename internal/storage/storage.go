package storage

import (
	"fmt"
	"sync"

	"github.com/ela-lab/razpravljalnica/api"
)

// UserAlreadyExistsError is returned when trying to create a user with a name that already exists
type UserAlreadyExistsError struct {
	Name string
}

func (e *UserAlreadyExistsError) Error() string {
	return fmt.Sprintf("user with name '%s' already exists", e.Name)
}

type UserStorage struct {
	dict    map[int64]*api.User
	nameIdx map[string]int64 // username -> user ID for fast lookup
	lock    sync.RWMutex
}

func NewUserStorage() *UserStorage {
	return &UserStorage{
		dict:    make(map[int64]*api.User),
		nameIdx: make(map[string]int64),
	}
}

type TopicStorage struct {
	dict map[int64]*api.Topic //topic_id -> topic
	lock sync.RWMutex
}

func NewTopicStorage() *TopicStorage {
	return &TopicStorage{
		dict: make(map[int64]*api.Topic),
	}
}

type SubscriptionStorage struct {
	dict map[int64][]int64 //user_id -> seznam topic_id
	lock sync.RWMutex
}

func NewSubscriptionStorage() *SubscriptionStorage {
	return &SubscriptionStorage{
		dict: make(map[int64][]int64),
	}
}

type MessageStorage struct {
	dict map[int64][]*api.Message //topic_id -> seznam message
	lock sync.RWMutex
}

func NewMessageStorage() *MessageStorage {
	return &MessageStorage{
		dict: make(map[int64][]*api.Message),
	}
}

type LikeStorage struct {
	dict map[int64][]int64 //message_id -> seznam user_id
	lock sync.RWMutex
}

func NewLikeStorage() *LikeStorage {
	return &LikeStorage{
		dict: make(map[int64][]int64),
	}
}

// USERS
func (us *UserStorage) CreateUser(user *api.User, ret *struct{}) error {
	us.lock.Lock()
	defer us.lock.Unlock()

	// Check if username already exists
	if _, exists := us.nameIdx[user.Name]; exists {
		return &UserAlreadyExistsError{Name: user.Name}
	}

	us.dict[user.Id] = user
	us.nameIdx[user.Name] = user.Id
	return nil
}

func (us *UserStorage) ReadUser(id int64, name *string) error {
	us.lock.RLock()
	defer us.lock.RUnlock()
	if user, ok := us.dict[id]; ok {
		*name = user.Name
	}
	return nil
}

// LIKES
func (ls *LikeStorage) CreateLike(like *api.Like, ret *struct{}) error {
	ls.lock.Lock()
	defer ls.lock.Unlock()
	_, keyExists := ls.dict[like.MessageId]
	if keyExists == false {
		//še ni nobenega like-a za to sporočilo -> ustvarimo prazen slice
		ls.dict[like.MessageId] = make([]int64, 0)
	}

	//preveri, če like za user-ja že obstaja
	for _, user_id := range ls.dict[like.MessageId] {
		if user_id == like.UserId {
			return nil
		}
	}

	ls.dict[like.MessageId] = append(ls.dict[like.MessageId], like.UserId)
	return nil
}

// ToggleLike adds a like if absent, or removes it if already present.
// Returns true if now liked, false if unliked.
func (ls *LikeStorage) ToggleLike(like *api.Like, likedNow *bool) error {
	ls.lock.Lock()
	defer ls.lock.Unlock()

	users, ok := ls.dict[like.MessageId]
	if !ok {
		ls.dict[like.MessageId] = []int64{like.UserId}
		*likedNow = true
		return nil
	}

	// check if already liked
	for i, uid := range users {
		if uid == like.UserId {
			// remove
			users = append(users[:i], users[i+1:]...)
			ls.dict[like.MessageId] = users
			*likedNow = false
			return nil
		}
	}

	// add
	ls.dict[like.MessageId] = append(users, like.UserId)
	*likedNow = true
	return nil
}

// št. like-ov na message
func (ls *LikeStorage) ReadLikes(messageId int64, likes *int64) error {
	ls.lock.RLock()
	defer ls.lock.RUnlock()
	users, ok := ls.dict[messageId]
	if ok {
		*likes = int64(len(users))
	} else {
		*likes = 0
	}
	return nil
}

// MESSAGES
func (ms *MessageStorage) CreateMessage(message *api.Message, ret *struct{}) error {
	ms.lock.Lock()
	defer ms.lock.Unlock()
	//če še message v topic ne obstaja
	if _, ok := ms.dict[message.TopicId]; !ok {
		ms.dict[message.TopicId] = []*api.Message{}
	}
	ms.dict[message.TopicId] = append(ms.dict[message.TopicId], message)
	return nil
}

func (ms *MessageStorage) DeleteMessage(messageId, topicId int64, ret *struct{}) error {
	ms.lock.Lock()
	defer ms.lock.Unlock()
	msgs, ok := ms.dict[topicId]
	if !ok {
		return nil //tema ne obstaja
	}

	//nov slice za filtriranje sporočil
	newMsgs := []*api.Message{}
	for _, m := range msgs {
		if m.Id != messageId {
			newMsgs = append(newMsgs, m)
		}
	}

	ms.dict[topicId] = newMsgs
	return nil
}

func (ms *MessageStorage) ReadMessages(topicId int64, dict *[]*api.Message) error {
	ms.lock.RLock()
	defer ms.lock.RUnlock()

	msgs, ok := ms.dict[topicId]
	if !ok {
		*dict = []*api.Message{}
		return nil
	}

	*dict = make([]*api.Message, len(msgs))
	copy(*dict, msgs)

	return nil
}

func (ms *MessageStorage) UpdateMessage(message *api.Message, ret *struct{}) error {
	ms.lock.Lock()
	defer ms.lock.Unlock()
	if msgs, ok := ms.dict[message.TopicId]; ok {
		for i, j := range msgs {
			if j.Id == message.Id {
				ms.dict[message.TopicId][i] = message
				return nil
			}
		}
	}
	return nil
}

// SUBSCRIPTIONS
func (ss *SubscriptionStorage) CreateSubscription(userId, topicId int64, ret *struct{}) error {
	ss.lock.Lock()
	defer ss.lock.Unlock()
	if topics, ok := ss.dict[userId]; ok {
		for _, j := range topics {
			if j == topicId {
				return nil
			}
		}
		ss.dict[userId] = append(ss.dict[userId], topicId)
	} else {
		ss.dict[userId] = []int64{topicId}
	}
	return nil
}

// ReadSubscriptions returns the list of topic IDs for a user
func (ss *SubscriptionStorage) ReadSubscriptions(userId int64, topics *[]int64) error {
	ss.lock.RLock()
	defer ss.lock.RUnlock()

	if subs, ok := ss.dict[userId]; ok {
		*topics = append([]int64(nil), subs...)
	} else {
		*topics = []int64{}
	}
	return nil
}

// TOPICS
func (ts *TopicStorage) CreateTopic(topic *api.Topic, ret *struct{}) error {
	ts.lock.Lock()
	defer ts.lock.Unlock()
	ts.dict[topic.Id] = topic
	return nil
}

func (ts *TopicStorage) ReadTopics(topics *[]*api.Topic) error {
	ts.lock.RLock()
	defer ts.lock.RUnlock()
	*topics = []*api.Topic{}
	for _, val := range ts.dict {
		*topics = append(*topics, val)
	}
	return nil
}
