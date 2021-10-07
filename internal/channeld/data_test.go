package channeld

import (
	"container/list"
	"log"
	"net"
	"testing"
	"time"

	"clewcat.com/channeld/proto"
	"github.com/indiest/fmutils"
	"github.com/stretchr/testify/assert"
	protobuf "google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

type TestUpdateMessageSender struct {
	queue []*proto.TestChannelDataMessage
}

func (s *TestUpdateMessageSender) Send(c *Connection, channelId ChannelId, msgType proto.MessageType, msg Message) {
	// Extract the payload from the ChannelDataUpdatMessage
	payload := msg.(*proto.ChannelDataUpdateMessage).Data
	updateMsg, err := payload.UnmarshalNew()
	payload.ProtoReflect()
	if err != nil {
		log.Panicln(err)
	}
	s.queue = append(s.queue, updateMsg.(*proto.TestChannelDataMessage))
}

func addTestConnection(t ConnectionType) *Connection {
	conn1, _ := net.Pipe()
	c := AddConnection(conn1, t)
	c.sender = &TestUpdateMessageSender{queue: make([]*proto.TestChannelDataMessage, 0)}
	return c
}

func (c *Connection) testQueue() []*proto.TestChannelDataMessage {
	return c.sender.(*TestUpdateMessageSender).queue
}

func (c *Connection) latestMsg() *proto.TestChannelDataMessage {
	queue := c.testQueue()
	if len(queue) > 0 {
		return queue[len(queue)-1]
	} else {
		return nil
	}
}

// See the test case in [the design doc](doc/design.md#fan-out)
func TestFanOutChannelData(t *testing.T) {
	InitConnections(3, "../../config/server_conn_fsm.json", "../../config/client_conn_fsm.json")
	InitChannels()

	c0 := addTestConnection(SERVER)
	c1 := addTestConnection(CLIENT)
	c2 := addTestConnection(CLIENT)

	testChannel := CreateChannel(proto.ChannelType_TEST, c0)
	dataMsg := &proto.TestChannelDataMessage{
		Text: "a",
		Num:  1,
	}
	testChannel.data = &ChannelData{
		msg:             dataMsg,
		updateMsgBuffer: list.New(),
	}
	// We need to manually tick the channel. Set the interval to a very large value.
	testChannel.tickInterval = time.Hour

	c0.SubscribeToChannel(testChannel, nil)
	c1.SubscribeToChannel(testChannel, &proto.ChannelSubscriptionOptions{
		FanOutIntervalMs: 50,
	})

	channelStartTime := ChannelTime(100 * int64(time.Millisecond))
	// F0 = the whole data
	testChannel.tickData(channelStartTime)
	assert.Equal(t, 1, len(c1.testQueue()))
	assert.Equal(t, 0, len(c2.testQueue()))
	assert.EqualValues(t, dataMsg.Num, c1.latestMsg().Num)

	c2.SubscribeToChannel(testChannel, &proto.ChannelSubscriptionOptions{
		FanOutIntervalMs: 100,
	})
	// F1 = no data, F7 = the whole data
	testChannel.tickData(channelStartTime.AddMs(50))
	assert.Equal(t, 1, len(c1.testQueue()))
	assert.Equal(t, 1, len(c2.testQueue()))
	assert.EqualValues(t, dataMsg.Num, c2.latestMsg().Num)

	// U1 arrives
	u1 := &proto.TestChannelDataMessage{Text: "b"}
	testChannel.Data().OnUpdate(u1, channelStartTime.AddMs(60))

	// F2 = U1
	testChannel.tickData(channelStartTime.AddMs(100))
	assert.Equal(t, 2, len(c1.testQueue()))
	assert.Equal(t, 1, len(c2.testQueue()))
	// U1 doesn't have "ClientConnNum" property
	assert.NotEqualValues(t, dataMsg.Num, c1.latestMsg().Num)
	assert.EqualValues(t, "b", c1.latestMsg().Text)
	assert.EqualValues(t, "a", c2.latestMsg().Text)

	// U2 arrives
	u2 := &proto.TestChannelDataMessage{Text: "c"}
	testChannel.Data().OnUpdate(u2, channelStartTime.AddMs(120))

	// F8=U1+U2; F3 = U2
	testChannel.tickData(channelStartTime.AddMs(150))
	assert.Equal(t, 3, len(c1.testQueue()))
	assert.Equal(t, 2, len(c2.testQueue()))
	assert.EqualValues(t, "c", c1.latestMsg().Text)
	assert.EqualValues(t, "c", c2.latestMsg().Text)
}

func TestListMoveElement(t *testing.T) {
	list := list.New()
	list.PushBack("a")
	list.PushBack("b")
	list.PushBack("c")
	assert.Equal(t, "a", list.Front().Value)

	e := list.Front().Next()
	assert.Equal(t, "b", e.Value)
	temp := e.Prev()
	list.MoveToBack(e)
	e = temp.Next()
	assert.Equal(t, "c", e.Value)
}

func TestDataMergeOptions(t *testing.T) {
	dstMsg := &proto.TestMergeMessage{
		List: []string{"a", "b", "c"},
		Kv: map[int64]*proto.TestMergeMessage_StringWrapper{
			1: {Content: "aa"},
			2: {Content: "bb"},
		},
	}

	srcMsg := &proto.TestMergeMessage{
		List: []string{"d", "e"},
		Kv: map[int64]*proto.TestMergeMessage_StringWrapper{
			1: nil,
			2: {Content: "bbb"},
		},
	}

	mergedMsg1 := protobuf.Clone(dstMsg).(*proto.TestMergeMessage)
	mergeOptions1 := &DataMergeOptions{
		ShouldReplaceRepeated: true,
	}
	mergeWithOptions(mergedMsg1, srcMsg, mergeOptions1)
	assert.Equal(t, 2, len(mergedMsg1.List))
	assert.Equal(t, "e", mergedMsg1.List[1])

	mergedMsg2 := protobuf.Clone(dstMsg).(*proto.TestMergeMessage)
	mergeOptions2 := &DataMergeOptions{
		ListSizeLimit: 4,
	}
	mergeWithOptions(mergedMsg2, srcMsg, mergeOptions2)
	assert.Equal(t, 4, len(mergedMsg2.List))
	assert.Equal(t, "b", mergedMsg2.List[0])

	mergedMsg3 := protobuf.Clone(dstMsg).(*proto.TestMergeMessage)
	mergeOptions3 := &DataMergeOptions{
		ShouldDeleteNilMapValue: true,
	}
	mergeWithOptions(mergedMsg3, srcMsg, mergeOptions3)
	assert.Equal(t, 1, len(mergedMsg3.Kv))
	assert.Nil(t, mergedMsg3.Kv[1])
	assert.Equal(t, "bbb", mergedMsg3.Kv[2].Content)
}

func TestReflectChannelData(t *testing.T) {
	globalData := ReflectChannelData(proto.ChannelType_TEST, nil)
	assert.NotNil(t, globalData)
	assert.IsType(t, &proto.TestChannelDataMessage{}, globalData.msg)
}

func TestDataFieldMasks(t *testing.T) {
	nestedMsg := &proto.TestFieldMaskMessage_NestedMessage{
		P1: 1,
		P2: 2,
	}
	testMsg := &proto.TestFieldMaskMessage{
		Name: "test",
		Msg:  nestedMsg,
		List: []*proto.TestFieldMaskMessage_NestedMessage{nestedMsg},
		Kv1: map[int64]*proto.TestFieldMaskMessage_NestedMessage{
			10: nestedMsg,
		},
		Kv2: map[int64]string{
			100: "hello",
		},
	}

	filteredMsg1 := protobuf.Clone(testMsg)
	fmutils.Filter(filteredMsg1, []string{"name"})
	t.Log(filteredMsg1.(*proto.TestFieldMaskMessage).String())

	filteredMsg2 := protobuf.Clone(testMsg)
	fmutils.Filter(filteredMsg2, []string{"msg.p1"})
	t.Log(filteredMsg2.(*proto.TestFieldMaskMessage).String())

	filteredMsg3 := protobuf.Clone(testMsg)
	fmutils.Filter(filteredMsg3, []string{"list.p2"})
	t.Log(filteredMsg3.(*proto.TestFieldMaskMessage).String())

	filteredMsg4 := protobuf.Clone(testMsg)
	fmutils.Filter(filteredMsg4, []string{"kv1.p1", "kv1.p2", "kv1.p3"})
	t.Log(filteredMsg4.(*proto.TestFieldMaskMessage).String())
	fmutils.Prune(filteredMsg4, []string{"kv1.p1"})
	t.Log(filteredMsg4.(*proto.TestFieldMaskMessage).String())

	filteredMsg5 := protobuf.Clone(testMsg)
	fmutils.Filter(filteredMsg5, []string{"kv2.a"})
	t.Log(filteredMsg5.(*proto.TestFieldMaskMessage).String())
}

func TestProtobufAny(t *testing.T) {
	any1, err := anypb.New(&proto.TestAnyMessage_Type1{Value: "a"})
	assert.NoError(t, err)

	any2, err := anypb.New(&proto.TestAnyMessage_Type2{Value: 1})
	assert.NoError(t, err)

	msg1 := &proto.TestAnyMessage{Msg: any1}
	msg2 := &proto.TestAnyMessage{Msg: any2}
	// Can merge the any property from different type
	protobuf.Merge(msg1, msg2)
	assert.EqualValues(t, any2, msg1.Msg)
	// Can be converted to a message of a unknown type
	um, err := msg1.Msg.UnmarshalNew()
	assert.NoError(t, err)
	assert.EqualValues(t, 1, um.(*proto.TestAnyMessage_Type2).Value)

	msg1.List = append(msg1.List, any1)
	msg2.List = append(msg2.List, any2)
	// Can merge the any list of different types
	protobuf.Merge(msg1, msg2)
	assert.Equal(t, 2, len(msg1.List))
}

func TestProtobufMapMerge(t *testing.T) {
	testMsg := &proto.TestMapMessage{
		Kv:  make(map[uint32]string),
		Kv2: make(map[uint32]*proto.TestMapMessage_StringWrapper),
	}
	testMsg.Kv[1] = "a"
	testMsg.Kv[2] = "b"
	testMsg.Kv[3] = "c"
	testMsg.Kv[4] = "d"

	testMsg.Kv2[1] = &proto.TestMapMessage_StringWrapper{Content: "a"}
	testMsg.Kv2[2] = &proto.TestMapMessage_StringWrapper{Content: "b"}

	updateMsg := &proto.TestMapMessage{
		Kv:  make(map[uint32]string),
		Kv2: make(map[uint32]*proto.TestMapMessage_StringWrapper),
	}
	updateMsg.Kv[2] = "bb"
	updateMsg.Kv[3] = ""
	updateMsg.Kv[4] = "dd"

	updateMsg.Kv2[1] = nil

	protobuf.Merge(testMsg, updateMsg)

	assert.Equal(t, "a", testMsg.Kv[1])
	assert.Equal(t, "bb", testMsg.Kv[2])
	assert.Equal(t, "", testMsg.Kv[3])
	assert.Equal(t, "dd", testMsg.Kv[4])

	/* By default, protobuf ignores the nil value
	assert.Equal(t, nil, testMsg.Kv2[1])
	*/
	assert.NotEqual(t, nil, testMsg.Kv2[1])
	assert.Equal(t, "b", testMsg.Kv2[2].Content)

}
