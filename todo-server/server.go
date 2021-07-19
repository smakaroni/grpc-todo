package main

import (
	"context"
	"fmt"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
	"grpc-blog/todopb"
	"log"
	"net"
	"os"
	"os/signal"
	"time"
)

type server struct{}

func (s server) DeleteTodo(ctx context.Context, request *todopb.DeleteTodoRequest) (*todopb.DeleteTodoResponse, error) {
	fmt.Printf("Delete todo %v\n", request.GetTodo().Title)
	oid, err := primitive.ObjectIDFromHex(request.GetTodo().GetId())
	if err != nil {
		return nil, status.Errorf(
			codes.InvalidArgument,
			fmt.Sprintf("Invalid argument %v\n", err))
	}
	filter := primitive.M{"_id": oid}

	data := &todopb.Todo{}

	res := collection.FindOne(nil, filter)
	if err := res.Decode(data); err != nil {
		return nil, status.Errorf(
			codes.NotFound,
			fmt.Sprintf("Todo not found %v\n", err))
	}

	data.Status = "DELETED"

	_, updErr := collection.ReplaceOne(nil, filter, data)
	if updErr != nil {
		return nil, status.Errorf(
			codes.Internal,
			fmt.Sprintf("cannot update todo %v\n", updErr))
	}

	return &todopb.DeleteTodoResponse{Todos: &todopb.Todo{
		Id:          data.Id,
		Title:       data.Title,
		Description: data.Description,
		Complete:    data.Complete,
		Status:      data.Status,
		Deadline:    data.Deadline,
	}}, nil
}

func (s server) ListTodo(ctx context.Context, request *todopb.ListTodoRequest) (*todopb.ListTodoResponse, error) {
	completed := false
	if !request.NotCompleted {
		completed = true
	}
	var todos []*todopb.Todo
	cur, err := collection.Find(nil, primitive.M{"complete": completed, "status": "NORMAL"})
	if err != nil {
		return nil, status.Errorf(
			codes.NotFound,
			fmt.Sprintf("Did not find any todos %v\n", err))
	}
	defer cur.Close(context.Background())

	for cur.Next(context.Background()) {
		todo := &todopb.Todo{}
		if err := cur.Decode(todo); err != nil {
			return nil, status.Errorf(
				codes.Internal,
				fmt.Sprintf("decoding data from db failed %v\n", err))
		}
		todos = append(todos, todo)
	}

	return &todopb.ListTodoResponse{Todos: todos}, nil

}

func (s server) CreateTodo(ctx context.Context, request *todopb.CreateTodoRequest) (*todopb.CreateTodoResponse, error) {

	data := todopb.Todo{
		Title:       request.Todo.Title,
		Description: request.Todo.Description,
		Complete:    false,
		Status:      "NORMAL",
		Deadline:    request.Todo.Deadline,
	}

	res, err := collection.InsertOne(nil, data)
	if err != nil {
		return nil, status.Errorf(
			codes.Internal,
			fmt.Sprintf("Internal error %v\n", err))
	}
	oid, ok := res.InsertedID.(primitive.ObjectID)
	if !ok {
		return nil, status.Errorf(
			codes.Internal,
			fmt.Sprintf("cannot convert to object id \n"))
	}
	request.Todo.Id = oid.Hex()

	return &todopb.CreateTodoResponse{Id: request.Todo.Id}, nil
}

func (s server) CreateTodos(ctx context.Context, request *todopb.CreateTodosRequest) (*todopb.CreateTodosResponse, error) {
	var ids []string
	for _, todo := range request.Todos {
		data := todopb.Todo{
			Title:       todo.Title,
			Description: todo.Description,
			Complete:    false,
			Deadline:    todo.Deadline,
		}
		res, err := collection.InsertOne(nil, data)
		if err != nil {
			return nil, status.Errorf(
				codes.Internal,
				fmt.Sprintf("internal error %v\n", err))
		}
		oid, ok := res.InsertedID.(primitive.ObjectID)
		if !ok {
			return nil, status.Errorf(
				codes.Internal,
				fmt.Sprintf("Cannot convert object id"))
		}
		ids = append(ids, oid.Hex())
	}
	return &todopb.CreateTodosResponse{Id: ids}, nil
}

var collection *mongo.Collection

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	fmt.Println("Todo service started")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	client, err := mongo.Connect(ctx, options.Client().ApplyURI("mongodb://localhost:27017"))
	if err != nil {
		log.Fatalf("mongo db error %v\n", err)
	}
	defer func() {
		if err = client.Disconnect(ctx); err != nil {
			panic(err)
		}
	}()

	collection = client.Database("mydb").Collection("todo")

	lis, err := net.Listen("tcp", "0.0.0.0:50051")
	if err != nil {
		log.Fatalf("Failed to listen %v\n", err)
	}

	s := grpc.NewServer()
	todopb.RegisterTodoServiceServer(s, &server{})
	reflection.Register(s)

	go func() {
		if err := s.Serve(lis); err != nil {
			log.Fatalf("Failed to serve %v\n", err)
		}
	}()

	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt)
	<-ch
	s.Stop()
	lis.Close()
}
