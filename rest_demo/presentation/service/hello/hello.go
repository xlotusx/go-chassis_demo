package hello

import (
	restful2 "github.com/go-chassis/go-chassis/v2/server/restful"
	"net/http"
)

type Presentation struct {
}

func (s *Presentation) SayHello(b *restful2.Context) {
	b.Write([]byte("get user id: " + b.ReadPathParameter("userid")))
}

func (s *Presentation) URLPatterns() []restful2.Route {
	return []restful2.Route{
		{Method: http.MethodPost, Path: "/sayhello/{userid}", ResourceFunc: s.SayHello,
			Returns: []*restful2.Returns{{Code: 200}}}}
}
