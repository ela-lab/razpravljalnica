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
	dict map[int64]razpravljalnica.Topic
	lock sync.RWMutex
}

type SubscriptionStorage struct {
	dict map[int64][]int64 // user_id -> seznam topic_id
    lock sync.RWMutex
}

type MessageStorage struct {
	dict map[int64]razpravljalnica.Message
	lock sync.RWMutex
}

type LikeStorage struct {
	dict map[int64][]int64 // message_id -> seznam user_id
	lock sync.RWMutex
}

/*
TO DO:
MESSAGE - Create, Update, Delete, Read(sporočila v temi)
SUBSCRIPTION - Create
TOPIC - Create, Read(vse teme)
*/

//USERS
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

//LIKES
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

//št. like-ov na message
func (ls *LikeStorage) ReadAllLikes(messageId int64, likes *int64) error {
	ls.lock.RLock()
	defer ls.lock.RUnlock()
	users, ok := ls.dict[messageId];
	if ok {
		*likes = int64(len(users))
	} else {
		*likes = 0
	}
	return nil
}

//MESSAGES

