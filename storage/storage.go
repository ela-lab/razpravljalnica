package storage

import (
	"sync"
	"github.com/ela-lab/razpravljalnica/razpravljalnica"
)

type UserStorage struct {
	dict map[int64]razpravljalnica.User
	lock sync.RWMutex
}

type TopicStorage struct {
	dict map[int64]razpravljalnica.Topic //topic_id -> topic
	lock sync.RWMutex
}

type SubscriptionStorage struct {
	dict map[int64][]int64 //user_id -> seznam topic_id
	lock sync.RWMutex
}

type MessageStorage struct {
	dict map[int64][]razpravljalnica.Message //topic_id -> seznam message
	lock sync.RWMutex
}

type LikeStorage struct {
	dict map[int64][]int64 //message_id -> seznam user_id
	lock sync.RWMutex
}

// USERS
func (us *UserStorage) CreateUser(user razpravljalnica.User, ret *struct{}) error {
	us.lock.Lock()
	defer us.lock.Unlock() //v vsakem primeru se izvede unlock
	us.dict[user.Id] = user
	return nil
}

func (us *UserStorage) ReadUser(id int64, name *string) error {
	us.lock.RLock()
	defer us.lock.RUnlock()
	*name = us.dict[id].Name
	return nil
}

// LIKES
func (ls *LikeStorage) CreateLike(like razpravljalnica.Like, ret *struct{}) error {
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
func (ms *MessageStorage) CreateMessage(message razpravljalnica.Message, ret *struct{}) error {
	ms.lock.Lock()
	defer ms.lock.Unlock()
	//če še message v topic ne obstaja
	if _, ok := ms.dict[message.TopicId]; !ok {
		ms.dict[message.TopicId] = []razpravljalnica.Message{}
	}
	ms.dict[message.TopicId] = append(ms.dict[message.TopicId], message)
	return nil
}

func (ms *MessageStorage) DeleteMessage(messageId, topicId int64, ret *struct{}) error {
	ms.lock.Lock()
	msgs, ok := ms.dict[topicId]
	if !ok {
		return nil //tema ne obstaja
	}

	//nov slice za filtriranje sporočil
	newMsgs := []razpravljalnica.Message{}
	for _, m := range msgs {
		if m.Id != messageId {
			newMsgs = append(newMsgs, m)
		}
	}

	ms.dict[topicId] = newMsgs
	return nil
}

func (ms *MessageStorage) ReadMessages(topicId int64, dict *[]razpravljalnica.Message) error {
	ms.lock.RLock()
	defer ms.lock.RUnlock()

	msgs, ok := ms.dict[topicId]
	if !ok {
		*dict = []razpravljalnica.Message{}
		return nil
	}

	*dict = make([]razpravljalnica.Message, len(msgs))
	copy(*dict, msgs)

	return nil
}

func (ms *MessageStorage) UpdateMessage(message razpravljalnica.Message, ret *struct{}) error {
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

//SUBSCRIPTIONS
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

//TOPICS
func (ts *TopicStorage) CreateTopic(topic razpravljalnica.Topic, ret *struct{}) error {
	ts.lock.Lock()
	defer ts.lock.Unlock()
	ts.dict[topic.Id] = topic
	return nil
}

func (ts *TopicStorage) ReadTopics(topics *[]razpravljalnica.Topic) error {
	ts.lock.RLock()
	defer ts.lock.RUnlock()
	*topics = []razpravljalnica.Topic{}
	for _, val := range ts.dict {
		*topics = append(*topics, val)
	}
	return nil
}