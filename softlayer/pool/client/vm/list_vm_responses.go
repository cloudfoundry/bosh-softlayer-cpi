package vm

// This file was generated by the swagger tool.
// Editing this file might prove futile when you re-run the swagger generate command

import (
	"fmt"
	"io"

	"github.com/go-openapi/runtime"

	strfmt "github.com/go-openapi/strfmt"

	"github.com/cloudfoundry/bosh-softlayer-cpi/softlayer/pool/models"
)

// ListVMReader is a Reader for the ListVM structure.
type ListVMReader struct {
	formats strfmt.Registry
}

// ReadResponse reads a server response into the received o.
func (o *ListVMReader) ReadResponse(response runtime.ClientResponse, consumer runtime.Consumer) (interface{}, error) {
	switch response.Code() {

	case 200:
		result := NewListVMOK()
		if err := result.readResponse(response, consumer, o.formats); err != nil {
			return nil, err
		}
		return result, nil

	case 404:
		result := NewListVMNotFound()
		if err := result.readResponse(response, consumer, o.formats); err != nil {
			return nil, err
		}
		return nil, result

	default:
		result := NewListVMDefault(response.Code())
		if err := result.readResponse(response, consumer, o.formats); err != nil {
			return nil, err
		}
		return nil, result
	}
}

// NewListVMOK creates a ListVMOK with default headers values
func NewListVMOK() *ListVMOK {
	return &ListVMOK{}
}

/*ListVMOK handles this case with default header values.

successful operation
*/
type ListVMOK struct {
	Payload *models.VmsResponse
}

func (o *ListVMOK) Error() string {
	return fmt.Sprintf("[GET /vms][%d] listVmOK  %+v", 200, o.Payload)
}

func (o *ListVMOK) readResponse(response runtime.ClientResponse, consumer runtime.Consumer, formats strfmt.Registry) error {

	o.Payload = new(models.VmsResponse)

	// response payload
	if err := consumer.Consume(response.Body(), o.Payload); err != nil && err != io.EOF {
		return err
	}

	return nil
}

// NewListVMNotFound creates a ListVMNotFound with default headers values
func NewListVMNotFound() *ListVMNotFound {
	return &ListVMNotFound{}
}

/*ListVMNotFound handles this case with default header values.

vm not found
*/
type ListVMNotFound struct {
}

func (o *ListVMNotFound) Error() string {
	return fmt.Sprintf("[GET /vms][%d] listVmNotFound ", 404)
}

func (o *ListVMNotFound) readResponse(response runtime.ClientResponse, consumer runtime.Consumer, formats strfmt.Registry) error {

	return nil
}

// NewListVMDefault creates a ListVMDefault with default headers values
func NewListVMDefault(code int) *ListVMDefault {
	return &ListVMDefault{
		_statusCode: code,
	}
}

/*ListVMDefault handles this case with default header values.

unexpected error
*/
type ListVMDefault struct {
	_statusCode int

	Payload *models.Error
}

// Code gets the status code for the list Vm default response
func (o *ListVMDefault) Code() int {
	return o._statusCode
}

func (o *ListVMDefault) Error() string {
	return fmt.Sprintf("[GET /vms][%d] listVm default  %+v", o._statusCode, o.Payload)
}

func (o *ListVMDefault) readResponse(response runtime.ClientResponse, consumer runtime.Consumer, formats strfmt.Registry) error {

	o.Payload = new(models.Error)

	// response payload
	if err := consumer.Consume(response.Body(), o.Payload); err != nil && err != io.EOF {
		return err
	}

	return nil
}