package internal_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"github.com/d4l3k/messagediff"
	"github.com/hashicorp/go-hclog"
	. "github.com/naveego/plugin-oracle/internal"
	"github.com/naveego/plugin-oracle/internal/pub"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/types"
	"github.com/pkg/errors"
	"google.golang.org/grpc/metadata"
	"io"
	"os"
)

var _ = Describe("Host", func() {

	var (
		sut      pub.PublisherServer
		settings *Settings
	)

	BeforeEach(func() {

		log := hclog.New(&hclog.LoggerOptions{
			Level:      hclog.Trace,
			Output:     os.Stderr,
			JSONFormat: true,
		})

		sut = NewServer(log)

		settings = GetTestSettings()
	})

	Describe("Connect", func() {

		It("should succeed when connection is valid", func() {
			_, err := sut.Connect(context.Background(), pub.NewConnectRequest(settings))
			Expect(err).ToNot(HaveOccurred())
		})

		It("should error when connection is invalid", func() {
			settings.Form.Username = "a"
			_, err := sut.Connect(context.Background(), pub.NewConnectRequest(settings))
			Expect(err).To(HaveOccurred())
		})

		It("should error when settings are malformed", func() {
			_, err := sut.Connect(context.Background(), &pub.ConnectRequest{SettingsJson: "{"})
			Expect(err).To(HaveOccurred())
		})

	})

	Describe("DiscoverShapes", func() {

		BeforeEach(func() {
			Expect(sut.Connect(context.Background(), pub.NewConnectRequest(settings))).ToNot(BeNil())
		})

		Describe("when mode is ALL", func() {

			It("should get tables and views", func() {

				response, err := sut.DiscoverShapes(context.Background(), &pub.DiscoverSchemasRequest{
					Mode: pub.DiscoverSchemasRequest_ALL,
				})
				Expect(err).ToNot(HaveOccurred())

				shapes := response.Schemas

				var ids []string
				for _, s := range shapes {
					ids = append(ids, s.Id)
				}
				Expect(ids).To(ContainElement(`"C##NAVEEGO"."TYPES"`), )
				Expect(ids).To(ContainElement(`"C##NAVEEGO"."PREPOST"`), )
				Expect(ids).To(ContainElement(`"C##NAVEEGO"."AGENTS"`), )
				Expect(ids).To(ContainElement(`"C##NAVEEGO"."CUSTOMERS"`), )
				Expect(ids).To(ContainElement(`"C##NAVEEGO"."ORDERS"`))

				Expect(shapes).To(HaveLen(5), "only tables and views should be returned")
			})

			Describe("shape details", func() {
				var agents *pub.Schema
				BeforeEach(func() {
					response, err := sut.DiscoverShapes(context.Background(), &pub.DiscoverSchemasRequest{
						Mode:       pub.DiscoverSchemasRequest_ALL,
						SampleSize: 2,
					})
					Expect(err).ToNot(HaveOccurred())
					for _, s := range response.Schemas {
						if s.Id == `"C##NAVEEGO"."AGENTS"` {
							agents = s
						}
					}
					Expect(agents).ToNot(BeNil())
					Expect(agents.Errors).To(BeNil())

					agentsJSON, _ := json.Marshal(agents)
					fmt.Println("Agents JSON:", string(agentsJSON))
				})

				It("should include properties", func() {
					properties := agents.Properties

					Expect(properties).To(ContainProperty(&pub.Property{
						Id:           `"AGENT_CODE"`,
						Name:         "AGENT_CODE",
						Type:         pub.PropertyType_STRING,
						TypeAtSource: "CHAR(4)",
						// Oracle can't tell us if it's a key
						// IsKey:        true,
						IsNullable: false,
					}))
					Expect(properties).To(ContainProperty(&pub.Property{
						Id:           `"COMMISSION"`,
						Name:         "COMMISSION",
						Type:         pub.PropertyType_FLOAT,
						TypeAtSource: "BINARY_FLOAT",
						IsNullable:   true,
					}))
					Expect(properties).To(ContainProperty(&pub.Property{
						Id:           `"BIOGRAPHY"`,
						Name:         "BIOGRAPHY",
						Type:         pub.PropertyType_TEXT,
						TypeAtSource: "VARCHAR2(2056)",
						IsNullable:   true,
					}))
					Expect(properties).To(ContainProperty(&pub.Property{
						Id:           `"UPDATED_AT"`,
						Name:         "UPDATED_AT",
						Type:         pub.PropertyType_DATETIME,
						TypeAtSource: "TIMESTAMP WITH TIME ZONE",
						IsNullable: true,
					}))
				})

				It("should include count", func() {
					Expect(agents.Count).To(Equal(&pub.Count{
						Kind:  pub.Count_EXACT,
						Value: 12,
					}))
				})

			})

		})

		Describe("when mode is REFRESH", func() {

			Describe("when shape is defined by source", func() {
				var agentsSchema *pub.Schema

				BeforeEach(func() {
					refreshShape := &pub.Schema{
						Id:   `"C##NAVEEGO"."AGENTS"`,
						Name: "Agents",
					}

					response, err := sut.DiscoverShapes(context.Background(), &pub.DiscoverSchemasRequest{
						Mode:       pub.DiscoverSchemasRequest_REFRESH,
						ToRefresh:  []*pub.Schema{refreshShape},
						SampleSize: 2,
					})
					Expect(err).ToNot(HaveOccurred())
					shapes := response.Schemas
					Expect(shapes).To(HaveLen(1), "only requested shape should be returned")
					agentsSchema = response.Schemas[0]
					Expect(agentsSchema.Errors).To(BeNil())
				})

				It("should update shape", func() {
					properties := agentsSchema.Properties
					Expect(properties).To(ContainProperty(&pub.Property{
						Id:           `"AGENT_CODE"`,
						Name:         "AGENT_CODE",
						Type:         pub.PropertyType_STRING,
						TypeAtSource: "CHAR(4)",
						// Oracle can't tell us if it's a key
						// IsKey:        true,
						IsNullable: false,
					}))
					Expect(properties).To(ContainProperty(&pub.Property{
						Id:           `"COMMISSION"`,
						Name:         "COMMISSION",
						Type:         pub.PropertyType_FLOAT,
						TypeAtSource: "BINARY_FLOAT",
						IsNullable:   true,
					}))
				})

				It("should include sample", func() {
					Expect(agentsSchema.Sample).To(HaveLen(2))
				})
			})

			Describe("when shape is defined by query", func() {
				var schema *pub.Schema

				BeforeEach(func() {
					refreshShape := &pub.Schema{
						Id:    "agent_names",
						Name:  "Agent Names",
						Query: "SELECT AGENT_CODE, AGENT_NAME AS Name FROM Agents",
					}

					response, err := sut.DiscoverShapes(context.Background(), &pub.DiscoverSchemasRequest{
						Mode:       pub.DiscoverSchemasRequest_REFRESH,
						ToRefresh:  []*pub.Schema{refreshShape},
						SampleSize: 2,
					})
					Expect(err).ToNot(HaveOccurred())
					shapes := response.Schemas
					Expect(shapes).To(HaveLen(1), "only requested shape should be returned")
					schema = response.Schemas[0]
					Expect(schema.Errors).To(BeNil())
				})

				It("should update shape", func() {

					properties := schema.Properties
					Expect(properties).To(ContainProperty(&pub.Property{
						Id:           `"AGENT_CODE"`,
						Name:         "AGENT_CODE",
						Type:         pub.PropertyType_STRING,
						TypeAtSource: "CHAR(4)",
						IsKey:        false,
					}))
					Expect(properties).To(ContainProperty(&pub.Property{
						Id:           `"NAME"`,
						Name:         "NAME",
						Type:         pub.PropertyType_STRING,
						TypeAtSource: "VARCHAR2(40)",
						IsNullable:   true,
					}))
				})

				It("should include sample", func() {
					Expect(schema.Sample).To(HaveLen(2))
				})

				It("should include count", func() {
					Expect(schema.Count.Value).To(Equal(int32(12)))
				})
			})

		})

		Describe("PublishStream", func() {

			// Describe("pre and post publish queries", func() {
			//
			// 	var req *pub.PublishRequest
			//
			// 	setup := func(settings Settings) {
			// 		var prepost *pub.Shape
			// 		_, err := sut.Connect(context.Background(), pub.NewConnectRequest(settings))
			// 		Expect(err).ToNot(HaveOccurred())
			//
			// 		response, err := sut.DiscoverShapes(context.Background(), &pub.DiscoverShapesRequest{
			// 			Mode:       pub.DiscoverShapesRequest_ALL,
			// 		})
			// 		Expect(err).ToNot(HaveOccurred())
			// 		for _, s := range response.Shapes {
			// 			if s.Id == `"C##NAVEEGO"."PREPOST"` {
			// 				prepost = s
			// 			}
			// 		}
			// 		Expect(prepost).ToNot(BeNil())
			// 		req = &pub.PublishRequest{
			// 			Shape: prepost,
			// 		}
			//
			// 		Expect(db.Exec("delete from C##NAVEEGO.PREPOST")).ToNot(BeNil())
			// 		Expect(db.Exec("insert into C##NAVEEGO.PREPOST values ('placeholder')")).ToNot(BeNil())
			// 	}
			//
			// 	It("should run pre-publish query", func() {
			// 		settings.PrePublishQuery = "INSERT INTO C##NAVEEGO.PREPOST VALUES ('pre')"
			// 		setup(settings)
			//
			// 		stream := new(publisherStream)
			// 		Expect(sut.PublishStream(req, stream)).To(Succeed())
			// 		Expect(stream.err).ToNot(HaveOccurred())
			// 		Expect(stream.records).To(
			// 			ContainElement(
			// 				WithTransform(func(e *pub.Record) string { return e.DataJson },
			// 					ContainSubstring("pre"))))
			// 	})
			//
			// 	It("should run post-publish query", func() {
			// 		settings.PostPublishQuery = "INSERT INTO C##NAVEEGO.PREPOST VALUES ('post')"
			// 		setup(settings)
			// 		stream := new(publisherStream)
			// 		Expect(sut.PublishStream(req, stream)).To(Succeed())
			//
			// 		row := db.QueryRow("select * from C##NAVEEGO.PREPOST where Message = 'post'")
			// 		var msg string
			// 		Expect(row.Scan(&msg)).To(Succeed())
			// 		Expect(msg).To(Equal("post"))
			// 	})
			//
			// 	FIt("should run post-publish query even if publish fails", func() {
			// 		settings.PostPublishQuery = "INSERT INTO C##NAVEEGO.PREPOST VALUES ('post')"
			// 		setup(settings)
			// 		stream := new(publisherStream)
			// 		stream.err = errors.New("expected")
			//
			// 		Expect(sut.PublishStream(req, stream)).To(MatchError(ContainSubstring("expected")))
			//
			// 		row := db.QueryRow("select * from C##NAVEEGO.PREPOST where Message = 'post'")
			// 		var msg string
			// 		Expect(row.Scan(&msg)).To(Succeed())
			// 		Expect(msg).To(Equal("post"))
			// 	})
			//
			// 	It("should combine post-publish query error with publish error if publish fails", func() {
			// 		settings.PostPublishQuery = "INSERT INTO C##NAVEEGO.PREPOST 'invalid syntax'"
			// 		setup(settings)
			// 		stream := new(publisherStream)
			// 		stream.err = errors.New("expected")
			//
			// 		Expect(sut.PublishStream(req, stream)).To(
			// 			MatchError(
			// 				And(
			// 					ContainSubstring("expected"),
			// 					ContainSubstring("invalid"),
			// 				)))
			// 	})
			// })

			Describe("filtering", func() {

				var req *pub.ReadRequest
				BeforeEach(func() {
					var agents *pub.Schema

					response, err := sut.DiscoverShapes(context.Background(), &pub.DiscoverSchemasRequest{
						Mode:       pub.DiscoverSchemasRequest_ALL,
						SampleSize: 2,
					})
					Expect(err).ToNot(HaveOccurred())
					for _, s := range response.Schemas {
						if s.Id == `"C##NAVEEGO"."AGENTS"` {
							agents = s
						}
					}
					Expect(agents).ToNot(BeNil())
					req = &pub.ReadRequest{
						Schema: agents,
					}
				})

				It("should publish all when unfiltered", func() {
					stream := new(publisherStream)
					Expect(sut.PublishStream(req, stream)).To(Succeed())
					Expect(stream.err).ToNot(HaveOccurred())
					Expect(stream.records).To(HaveLen(12))

					var alex map[string]interface{}
					var data []map[string]interface{}
					for _, record := range stream.records {
						var d map[string]interface{}
						Expect(json.Unmarshal([]byte(record.DataJson), &d)).To(Succeed())
						data = append(data, d)
						if d[`"AGENT_NAME"`] == "Alex" {
							alex = d
						}
					}
					Expect(alex).ToNot(BeNil(), "should find Alex (code==A003)")

					Expect(alex).To(And(
						HaveKeyWithValue(`"AGENT_CODE"`, "A003"),
						HaveKeyWithValue(`"AGENT_NAME"`, "Alex"),
						HaveKeyWithValue(`"WORKING_AREA"`, "London"),
						HaveKeyWithValue(`"COMMISSION"`, float64(0.13)),
						HaveKeyWithValue(`"PHONE_NO"`, "075-12458969"),
						HaveKeyWithValue(`"UPDATED_AT"`, "1969-01-02T00:00:00-05:00"),
						HaveKeyWithValue(`"BIOGRAPHY"`, ""),
					))
				})

				It("should filter on equality", func() {
					stream := new(publisherStream)
					req.Filters = []*pub.PublishFilter{
						{
							Kind:       pub.PublishFilter_EQUALS,
							PropertyId: `"AGENT_CODE"`,
							Value:      "A003",
						},
					}
					Expect(sut.PublishStream(req, stream)).To(Succeed())
					Expect(stream.err).ToNot(HaveOccurred())
					Expect(stream.records).To(HaveLen(1))
					Expect(stream.records[0].DataJson).To(ContainSubstring("Alex"))
				})

				It("should filter on GREATER_THAN", func() {
					stream := new(publisherStream)
					req.Filters = []*pub.PublishFilter{
						{
							Kind:       pub.PublishFilter_GREATER_THAN,
							PropertyId: `"UPDATED_AT"`,
							Value:      "1970-01-02T00:00:00Z",
						},
					}
					Expect(sut.PublishStream(req, stream)).To(Succeed())
					Expect(stream.err).ToNot(HaveOccurred())
					Expect(stream.records).To(HaveLen(7))
				})
				It("should filter on LESS_THAN", func() {
					stream := new(publisherStream)
					req.Filters = []*pub.PublishFilter{
						{
							Kind:       pub.PublishFilter_LESS_THAN,
							PropertyId: `"COMMISSION"`,
							Value:      "0.12",
						},
					}
					Expect(sut.PublishStream(req, stream)).To(Succeed())
					Expect(stream.err).ToNot(HaveOccurred())
					Expect(stream.records).To(HaveLen(2))
				})
			})

			Describe("typing", func() {

				var req *pub.ReadRequest
				BeforeEach(func() {
					var types *pub.Schema

					response, err := sut.DiscoverShapes(context.Background(), &pub.DiscoverSchemasRequest{
						Mode: pub.DiscoverSchemasRequest_REFRESH,
						ToRefresh: []*pub.Schema{
							{
								Id:   `"C##NAVEEGO"."TYPES"`,
								Name: "Types",
							},
						},
						SampleSize: 2,
					})
					Expect(err).ToNot(HaveOccurred())

					Expect(response.Schemas).To(HaveLen(1))
					types = response.Schemas[0]
					Expect(types).ToNot(BeNil())
					Expect(types.Errors).To(Or(BeNil(), HaveLen(0)))
					req = &pub.ReadRequest{
						Schema: types,
					}
				})

				It("should publish record with all data in correct format", func() {
					stream := new(publisherStream)
					Expect(sut.PublishStream(req, stream)).To(Succeed())
					Expect(stream.err).ToNot(HaveOccurred())
					Expect(stream.records).To(HaveLen(1))
					record := stream.records[0]
					var data map[string]interface{}
					Expect(json.Unmarshal([]byte(record.DataJson), &data)).To(Succeed())

					Expect(data).To(And(
						HaveKeyWithValue(`"number"`, BeNumerically("==", 42)),                              // NUMBER NOT NULL PRIMARY KEY,
						HaveKeyWithValue(`"float"`, BeNumerically("~", 123456.789, 1E8)),                   // BINARY_FLOAT,
						HaveKeyWithValue(`"double"`, BeNumerically("~", 123456.789, 1E8)),                  // BINARY_DOUBLE,
						HaveKeyWithValue(`"date"`, "1998-12-25T00:00:00Z"),                                 // DATE,
						HaveKeyWithValue(`"timestamp"`, "1997-01-31T09:26:56.66Z"),                    // TIMESTAMP,
						HaveKeyWithValue(`"timestampWithTimeZone"`, "1997-01-31T09:26:56.66+02:00"),       // TIMESTAMP WITH TIME ZONE,
						HaveKeyWithValue(`"intervalYear4ToMonth"`, "+02-04"),                                // INTERVAL YEAR (2) TO MONTH,
						HaveKeyWithValue(`"intervalDay4ToSecond2"`, "+0120 06:31:14.00"),                         // INTERVAL DAY (4) TO SECOND (2),
						HaveKeyWithValue(`"char"`, "char  "),                                                 // CHAR(6),
						HaveKeyWithValue(`"varchar2"`, "varchar2"),                                         // VARCHAR2(10),
						HaveKeyWithValue(`"nvarchar2"`, "nvarchar2"),                                       // NVARCHAR2(10),
						HaveKeyWithValue(`"nchar"`, "nchar "),                                               // NCHAR(6),
						HaveKeyWithValue(`"xml"`, "<data>42</data>\n"),                                       // XMLTYPE,
						HaveKeyWithValue(`"blob"`, base64.StdEncoding.EncodeToString([]byte("blob data"))), // BLOB,
						HaveKeyWithValue(`"clob"`, "clob"),                                                 // CLOB,
						HaveKeyWithValue(`"nclob"`, "nclobdata"),                                           // NCLOB
					))

				})

				Describe("Disconnect", func() {

					It("should not be connected after disconnect", func() {
						Expect(sut.Disconnect(context.Background(), &pub.DisconnectRequest{})).ToNot(BeNil())

						_, err := sut.DiscoverShapes(context.Background(), &pub.DiscoverSchemasRequest{})
						Expect(err).To(MatchError(ContainSubstring("not connected")))

						err = sut.PublishStream(&pub.ReadRequest{}, nil)
						Expect(err).To(MatchError(ContainSubstring("not connected")))
					})

				})

			})
		})
	})

	Describe("Write Backs", func() {

		BeforeEach(func() {
			Expect(sut.Connect(context.Background(), pub.NewConnectRequest(settings))).ToNot(BeNil())
		})

		Describe("ConfigureWrite", func() {

			var req *pub.ConfigureWriteRequest
			BeforeEach(func() {
				req = &pub.ConfigureWriteRequest{}
			})

			It("should return a json form schema on the first call", func() {
				response, err := sut.ConfigureWrite(context.Background(), req)
				Expect(err).ToNot(HaveOccurred())

				Expect(response.Form).ToNot(BeNil())
				Expect(response.Form.SchemaJson).ToNot(BeNil())
				Expect(response.Form.UiJson).ToNot(BeNil())
				Expect(response.Schema).To(BeNil())
			})

			It("should return a schema when a valid stored procedure is input", func() {
				req.Form = &pub.ConfigurationFormRequest{
					DataJson: `{"storedProcedure":"C##NAVEEGO.TEST"}`,
				}

				response, err := sut.ConfigureWrite(context.Background(), req)
				Expect(err).ToNot(HaveOccurred())

				Expect(response.Form).ToNot(BeNil())
				Expect(response.Schema).ToNot(BeNil())

				Expect(response.Schema.Id).To(Equal("C##NAVEEGO.TEST"))
				Expect(response.Schema.Query).To(Equal("C##NAVEEGO.TEST"))
				Expect(response.Schema.Properties).To(HaveLen(3))
				Expect(response.Schema.Properties[0].Id).To(Equal("I_AGENTID"))
				Expect(response.Schema.Properties[1].Id).To(Equal("I_NAME"))
				Expect(response.Schema.Properties[2].Id).To(Equal("I_COMMISSION"))
				Expect(response.Schema.Properties[2].Type).To(Equal(pub.PropertyType_FLOAT))
			})

			It("should return a schema when a valid stored procedure with schema is input", func() {
				req.Form = &pub.ConfigurationFormRequest{
					DataJson: `{"storedProcedure":"C##NAVEEGO.TEST"}`,
				}

				response, err := sut.ConfigureWrite(context.Background(), req)
				Expect(err).ToNot(HaveOccurred())

				Expect(response.Form).ToNot(BeNil())
				Expect(response.Schema).ToNot(BeNil())

				Expect(response.Schema.Id).To(Equal("C##NAVEEGO.TEST"))
				Expect(response.Schema.Query).To(Equal("C##NAVEEGO.TEST"))
				Expect(response.Schema.Properties).To(HaveLen(3))
				Expect(response.Schema.Properties[0].Id).To(Equal("I_AGENTID"))
				Expect(response.Schema.Properties[1].Id).To(Equal("I_NAME"))
				Expect(response.Schema.Properties[2].Id).To(Equal("I_COMMISSION"))
				Expect(response.Schema.Properties[2].Type).To(Equal(pub.PropertyType_FLOAT))
			})

			It("should return an error when an invalid stored procedure is input", func() {
				req.Form = &pub.ConfigurationFormRequest{
					DataJson: `{"storedProcedure":"NOT A PROC"}`,
				}

				response, err := sut.ConfigureWrite(context.Background(), req)
				Expect(err).ToNot(HaveOccurred())

				Expect(response.Form).ToNot(BeNil())
				Expect(response.Schema).ToNot(BeNil())
				Expect(response.Form.Errors).To(HaveLen(1))
				Expect(response.Form.Errors[0]).To(ContainSubstring("stored procedure does not exist"))
			})
		})

		Describe("PrepareWrite", func() {

			var req *pub.PrepareWriteRequest
			BeforeEach(func() {
				req = &pub.PrepareWriteRequest{
					Schema: &pub.Schema{},
					CommitSlaSeconds: 1,
				}
			})

			It("should prepare the plugin to write", func() {
				response, err := sut.PrepareWrite(context.Background(), req)
				Expect(err).ToNot(HaveOccurred())
				Expect(response).ToNot(BeNil())
			})
		})

		Describe("WriteStream", func() {

			var records []*pub.Record
			var stream *writeStream
			var req *pub.PrepareWriteRequest
			BeforeEach(func() {
				req =  &pub.PrepareWriteRequest{
					Schema: &pub.Schema{
						Id: "TEST",
						Query: "TEST",
						Properties: []*pub.Property {
							{
								Id: "AgentId",
							},
						},
					},
					CommitSlaSeconds: 1,
				}

				records = append(records, &pub.Record{
					DataJson: `{"AgentId":"A001"}`,
					CorrelationId: "test",
				})

				stream = &writeStream{
					records: records,
					index: 0,
				}
			})

			It("should be able to call a stored procedure to write a record", func() {
				response, err := sut.PrepareWrite(context.Background(), req)
				Expect(err).ToNot(HaveOccurred())
				Expect(response).ToNot(BeNil())

				Expect(sut.WriteStream(stream)).To(Succeed())

				Expect(stream.recordAcks).To(HaveLen(1))
				Expect(stream.recordAcks[0].CorrelationId).To(Equal("test"))
			})
		})
	})
})

type writeStream struct {
	records 	[]*pub.Record
	recordAcks 	[]*pub.RecordAck
	index 		int
	err     	error
}

func (p *writeStream) Send(ack *pub.RecordAck) error {
	if p.err != nil {
		return p.err
	}

	p.recordAcks = append(p.recordAcks, ack)
	return nil
}

func (p *writeStream) Recv() (*pub.Record, error) {
	if p.err != nil {
		return nil, p.err
	}

	if len(p.records) > p.index {
		record := p.records[p.index]
		p.index++
		return record, nil
	}

	return nil, io.EOF
}

func (writeStream) SetHeader(metadata.MD) error {
	panic("implement me")
}

func (writeStream) SendHeader(metadata.MD) error {
	panic("implement me")
}

func (writeStream) SetTrailer(metadata.MD) {
	panic("implement me")
}

func (writeStream) Context() context.Context {
	panic("implement me")
}

func (writeStream) SendMsg(m interface{}) error {
	panic("implement me")
}

func (writeStream) RecvMsg(m interface{}) error {
	panic("implement me")
}

type publisherStream struct {
	records []*pub.Record
	err     error
}

func (p *publisherStream) Send(record *pub.Record) error {
	if p.err != nil {
		return p.err
	}
	p.records = append(p.records, record)
	return nil
}

func (publisherStream) SetHeader(metadata.MD) error {
	panic("implement me")
}

func (publisherStream) SendHeader(metadata.MD) error {
	panic("implement me")
}

func (publisherStream) SetTrailer(metadata.MD) {
	panic("implement me")
}

func (publisherStream) Context() context.Context {
	panic("implement me")
}

func (publisherStream) SendMsg(m interface{}) error {
	panic("implement me")
}

func (publisherStream) RecvMsg(m interface{}) error {
	panic("implement me")
}

func ContainProperty(expected *pub.Property) types.GomegaMatcher {
	return &containPropertyMatcher{
		expected: expected,
	}
}

type containPropertyMatcher struct {
	expected *pub.Property
	best     *pub.Property
	left     string
	right string
}

func (m *containPropertyMatcher) Match(actual interface{}) (success bool, err error) {

	actuals, ok := actual.([]*pub.Property)
	if !ok {
		return false, errors.Errorf("actual type must be []*pub.Property, but got %T", actual)
	}

	for _, a := range actuals {

		if a.Id != m.expected.Id {
			continue
		}

		m.best = a

		var same bool
		m.left, same = messagediff.PrettyDiff(m.expected, m.best)

		if !same {
			m.right, _ = messagediff.PrettyDiff(m.best, m.expected)
		}

		return same, nil
	}

	return false, nil
}

func (m *containPropertyMatcher) FailureMessage(actual interface{}) (message string) {

	return fmt.Sprintf("Expected properties to match, but found diff: \nACTUAL:\n%s\n\nEXPECTED:\n%s", m.left, m.right)
}

func (m *containPropertyMatcher) NegatedFailureMessage(actual interface{}) (message string) {
	return "Expected properties to be different, but they were the same"
}
