package rules

import (
	"fmt"
	"math/rand"
	"time"

	"github.com/battlesnakeio/engine/controller/pb"
	log "github.com/sirupsen/logrus"
)

// GameTick runs the game one tick and updates the state
func GameTick(game *pb.Game, lastFrame *pb.GameFrame) (*pb.GameFrame, error) {
	if lastFrame == nil {
		return nil, fmt.Errorf("rules: invalid state, previous frame is nil")
	}
	nextFrame := &pb.GameFrame{
		Turn:   lastFrame.Turn + 1,
		Snakes: lastFrame.Snakes,
		Food:   lastFrame.Food,
	}
	duration := time.Duration(game.SnakeTimeout) * time.Millisecond
	log.WithFields(log.Fields{
		"GameID":  game.ID,
		"Turn":    nextFrame.Turn,
		"Timeout": duration,
	}).Info("GatherSnakeMoves")
	moves := GatherSnakeMoves(duration, game, lastFrame)

	// we have all the snake moves now
	// 1. update snake coords
	updateSnakes(game, nextFrame, moves)
	// 2. check for death
	// 	  a - starvation
	//    b - wall collision
	//    c - snake collision
	log.WithFields(log.Fields{
		"GameID": game.ID,
		"Turn":   nextFrame.Turn,
	}).Info("check for death")
	deathUpdates := checkForDeath(game.Width, game.Height, nextFrame)
	for _, du := range deathUpdates {
		if du.Snake.Death == nil {
			du.Snake.Death = du.Death
		}
	}
	// 3. game update
	//    a - turn incr -- done above when the next tick is created
	//    b - reduce health points
	//    c - grow snakes, and update snake health if they ate
	//    d - shrink snakes that didn't et
	//    e - remove eaten food
	//    f - replace eaten food
	log.WithFields(log.Fields{
		"GameID": game.ID,
		"Turn":   nextFrame.Turn,
	}).Info("reduce snake health")
	for _, s := range nextFrame.AliveSnakes() {
		s.Health = s.Health - 1
	}

	log.WithFields(log.Fields{
		"GameID": game.ID,
		"Turn":   nextFrame.Turn,
	}).Info("handle food")

	foodToRemove := checkForSnakesEating(nextFrame)
	nextFood, err := updateFood(game.Width, game.Height, lastFrame, foodToRemove)
	if err != nil {
		return nil, err
	}
	nextFrame.Food = nextFood
	return nextFrame, nil
}

func updateFood(width, height int32, gameFrame *pb.GameFrame, foodToRemove []*pb.Point) ([]*pb.Point, error) {
	food := []*pb.Point{}
	for _, foodPos := range gameFrame.Food {
		found := false
		for _, r := range foodToRemove {
			if foodPos.Equal(r) {
				found = true
				break
			}
		}

		if !found {
			food = append(food, foodPos)
		}
	}

	for range foodToRemove {
		p := getUnoccupiedPoint(width, height, gameFrame.Food, gameFrame.AliveSnakes())
		if p != nil {
			food = append(food, p)
		}
	}

	return food, nil
}

func getUnoccupiedPoint(width, height int32, food []*pb.Point, snakes []*pb.Snake) *pb.Point {
	openPoints := getUnoccupiedPoints(width, height, food, snakes)

	if len(openPoints) == 0 {
		return nil
	}

	randIndex := rand.Intn(len(openPoints))

	return openPoints[randIndex]
}

func getUnoccupiedPoints(width, height int32, food []*pb.Point, snakes []*pb.Snake) []*pb.Point {
	occupiedPoints := getUniqOccupiedPoints(food, snakes)

	numCandidatePoints := (width * height) - int32(len(occupiedPoints))

	candidatePoints := make([]*pb.Point, 0, numCandidatePoints)

	for x := int32(0); x < width; x++ {
		for y := int32(0); y < height; y++ {
			p := &pb.Point{X: x, Y: y}
			match := false

			for _, o := range occupiedPoints {
				if o.Equal(p) {
					match = true
					break
				}
			}

			if !match {
				candidatePoints = append(candidatePoints, p)
			}
		}
	}

	return candidatePoints
}

func getUniqOccupiedPoints(food []*pb.Point, snakes []*pb.Snake) []*pb.Point {
	occupiedPoints := []*pb.Point{}
	for _, f := range food {
		candidate := true
		for _, o := range occupiedPoints {
			if o.Equal(f) {
				candidate = false
				break
			}
		}
		if candidate {
			occupiedPoints = append(occupiedPoints, f)
		}
	}

	for _, s := range snakes {
		for _, b := range s.Body {
			candidate := true
			for _, o := range occupiedPoints {
				if o.Equal(b) {
					candidate = false
					break
				}
			}
			if candidate {
				occupiedPoints = append(occupiedPoints, b)
			}
		}
	}

	return occupiedPoints
}

func updateSnakes(game *pb.Game, frame *pb.GameFrame, moves []*SnakeUpdate) {
	for _, update := range moves {
		if update.Err != nil {
			log.WithFields(log.Fields{
				"GameID":  game.ID,
				"SnakeID": update.Snake.ID,
				"Name":    update.Snake.Name,
				"Turn":    frame.Turn,
			}).Info("Default move")
			update.Snake.DefaultMove()
		} else {
			log.WithFields(log.Fields{
				"GameID":  game.ID,
				"SnakeID": update.Snake.ID,
				"Name":    update.Snake.Name,
				"Turn":    frame.Turn,
				"Move":    update.Move,
			}).Info("Move")
			update.Snake.Move(update.Move)
		}
	}
}

func checkForSnakesEating(frame *pb.GameFrame) []*pb.Point {
	foodToRemove := []*pb.Point{}
	for _, snake := range frame.AliveSnakes() {
		ate := false
		for _, foodPos := range frame.Food {
			if snake.Head().Equal(foodPos) {
				snake.Health = 100
				ate = true
				foodToRemove = append(foodToRemove, foodPos)
			}
		}
		if !ate {
			if len(snake.Body) == 0 {
				continue
			}
			snake.Body = snake.Body[:len(snake.Body)-1]
		}
	}
	return foodToRemove
}
