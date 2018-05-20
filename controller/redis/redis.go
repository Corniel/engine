package redis

import (
	"context"
	"fmt"
	"time"

	"github.com/battlesnakeio/engine/controller"
	"github.com/gogo/protobuf/proto"
	"github.com/pkg/errors"

	"github.com/battlesnakeio/engine/controller/pb"
	"github.com/go-redis/redis"
	uuid "github.com/satori/go.uuid"
)

type Store struct {
	client *redis.Client
}

// NewStore will create a new instance of an underlying redis client, so it should not be re-created across "threads"
// - connectURL see: github.com/go-redis/redis/options.go for URL specifics
// The underlying redis client will be immediately tested for connectivity, so don't call this until you know redis can connect.
// Returns a new instance OR an error if unable (meaning an issue connecting to your redis URL)
func NewStore(connectURL string) (*Store, error) {
	o, err := redis.ParseURL(connectURL)
	if err != nil {
		return nil, errors.Wrap(err, "unable to parse redis URL")
	}

	client := redis.NewClient(o)

	// Validate it's connected
	err = client.Ping().Err()
	if err != nil {
		return nil, errors.Wrap(err, "unable to connect ")
	}

	return &Store{client: client}, nil
}

// Close closes the underlying redis client. see: github.com/go-redis/redis/Client.go
func (rs *Store) Close() error {
	return rs.client.Close()
}

// Lock will lock a specific game, returning a token that must be used to
// write frames to the game.
func (rs *Store) Lock(ctx context.Context, key, token string) (string, error) {
	// Generate a token if the one passed is empty
	if token == "" {
		token = uuid.NewV4().String()
	}

	// Acquire or match the lock token
	pipe := rs.client.TxPipeline()
	newLock := pipe.SetNX(gameLockKey(key), token, time.Minute)
	lockTkn := pipe.Get(gameLockKey(key))
	_, err := pipe.Exec()
	if err != nil {
		return "", errors.Wrap(err, "unexpected redis error during tx pipeline")
	}

	// Either we got a new lock or we have the same token for this to succeed
	if newLock.Val() || token == lockTkn.Val() {
		return lockTkn.Val(), nil
	}

	// Default pessimistically to no lock acquired
	return "", controller.ErrIsLocked
}

// Unlock will unlock a game if it is locked and the token used to lock it
// is correct.
func (rs *Store) Unlock(ctx context.Context, key, token string) error {
	// Short-circuit empty-string, we won't allow that
	if token == "" {
		return controller.ErrNotFound
	}

	r, err := UnlockCmd.Run(rs.client, []string{gameLockKey(key)}, token).Result()
	if err != nil {
		return errors.Wrap(err, "unexpected redis error during unlock")
	}

	// UnlockCmd returns a 1 if key was found
	if r.(int64) != 1 {
		return controller.ErrNotFound
	}

	return nil
}

// PopGameID returns a new game that is unlocked and running. Workers call
// this method through the controller to find games to process.
func (rs *Store) PopGameID(context.Context) (string, error) {
	return "", nil
}

// SetGameStatus is used to set a specific game status. This operation
// should be atomic.
func (rs *Store) SetGameStatus(c context.Context, id, status string) error {
	return nil
}

// CreateGame will insert a game with the default game frames.
func (rs *Store) CreateGame(context.Context, *pb.Game, []*pb.GameFrame) error {
	return nil
}

// PushGameFrame will push a game frame onto the list of frames.
func (rs *Store) PushGameFrame(c context.Context, id string, t *pb.GameFrame) error {
	frameBytes, err := proto.Marshal(t)
	if err != nil {
		return errors.Wrap(err, "frame marshalling error")
	}
	numAdded, err := rs.client.RPush(framesKey(id), frameBytes).Result()
	if err != nil {
		return errors.Wrap(err, "unexpected redis error")
	}
	if numAdded != 1 {
		return errors.Wrap(err, "unexpected redis result")
	}

	return nil
}

// ListGameFrames will list frames by an offset and limit, it supports
// negative offset.
func (rs *Store) ListGameFrames(c context.Context, id string, limit, offset int) ([]*pb.GameFrame, error) {
	if limit <= 0 {
		return nil, errors.Errorf("invalid limit %d", limit)
	}

	// Calculate list indexes
	start := int64(offset)
	end := int64(limit + offset)
	if offset <= 0 {
		end--
	}

	// Retrieve serialized frames
	frameData, err := rs.client.LRange(framesKey(id), start, end).Result()
	if err != nil {
		return nil, errors.Wrap(err, "unexpected redis error when getting frames")
	}

	// No frames
	if len(frameData) == 0 {
		return nil, nil
	}

	// Deserialize each frame
	frames := make([]*pb.GameFrame, len(frameData))
	for i, data := range frameData {
		var f pb.GameFrame
		err = proto.Unmarshal([]byte(data), &f)
		if err != nil {
			return nil, errors.Wrapf(err, "unable to unmarshal frame %s", data)
		}
		frames[i] = &f
	}

	return frames, nil
}

// GetGame will fetch the game.
func (rs *Store) GetGame(context.Context, string) (*pb.Game, error) {
	return nil, nil
}

var UnlockCmd = redis.NewScript(`
	if redis.call("GET", KEYS[1]) == ARGV[1] then
		redis.call("DEL", KEYS[1])
		return true
	end
	return false
`)

// generates the redis key for a game
func gameKey(gameID string) string {
	return fmt.Sprintf("games:%s:state", gameID)
}

// generates the redis key for game frames
func framesKey(gameID string) string {
	return fmt.Sprintf("games:%s:frames", gameID)
}

// generates the redis key for game lock state
func gameLockKey(gameID string) string {
	return fmt.Sprintf("games:%s:lock", gameID)
}