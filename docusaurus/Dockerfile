FROM node:lts

WORKDIR /app/website

EXPOSE 3000 35729
COPY ./docs /docs
COPY ./docusaurus/website /app/website
RUN yarn install

CMD ["yarn", "start"]
