package gcp

// openAPISpecTemplate is the base OpenAPI 3.0.1 specification template for API Gateway
// that routes traffic to Cloud Run backend and frontend services.
const openAPISpecTemplate = `openapi: 3.0.1
info:
  title: API Gateway for Cloud Run
  description: API Gateway routing to Cloud Run backend and frontend
  version: 1.0.0
servers:
  - url: https://{gateway_host}
paths:
  /api/{proxy+}:
    x-google-backend:
      address: %s/{proxy}
    get:
      operationId: apiProxyGet
      parameters:
        - name: proxy
          in: path
          required: true
          schema:
            type: string
      responses:
        '200':
          description: Successful response
          content:
            application/json:
              schema:
                type: object
        '404':
          description: Not found
    post:
      operationId: apiProxyPost
      parameters:
        - name: proxy
          in: path
          required: true
          schema:
            type: string
      requestBody:
        required: false
        content:
          application/json:
            schema:
              type: object
      responses:
        '200':
          description: Successful response
          content:
            application/json:
              schema:
                type: object
        '404':
          description: Not found
    put:
      operationId: apiProxyPut
      parameters:
        - name: proxy
          in: path
          required: true
          schema:
            type: string
      requestBody:
        required: false
        content:
          application/json:
            schema:
              type: object
      responses:
        '200':
          description: Successful response
          content:
            application/json:
              schema:
                type: object
        '404':
          description: Not found
    delete:
      operationId: apiProxyDelete
      parameters:
        - name: proxy
          in: path
          required: true
          schema:
            type: string
      responses:
        '200':
          description: Successful response
          content:
            application/json:
              schema:
                type: object
        '404':
          description: Not found
    options:
      operationId: apiProxyOptions
      parameters:
        - name: proxy
          in: path
          required: true
          schema:
            type: string
      responses:
        '200':
          description: CORS preflight response
          headers:
            Access-Control-Allow-Origin:
              schema:
                type: string
            Access-Control-Allow-Methods:
              schema:
                type: string
            Access-Control-Allow-Headers:
              schema:
                type: string
  /{proxy+}:
    x-google-backend:
      address: %s/{proxy}
    get:
      operationId: frontendProxyGet
      parameters:
        - name: proxy
          in: path
          required: true
          schema:
            type: string
      responses:
        '200':
          description: Successful response
          content:
            text/html:
              schema:
                type: string
        '404':
          description: Not found
    options:
      operationId: frontendProxyOptions
      parameters:
        - name: proxy
          in: path
          required: true
          schema:
            type: string
      responses:
        '200':
          description: CORS preflight response
          headers:
            Access-Control-Allow-Origin:
              schema:
                type: string
            Access-Control-Allow-Methods:
              schema:
                type: string
            Access-Control-Allow-Headers:
              schema:
                type: string`
