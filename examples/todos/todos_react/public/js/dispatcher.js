// Note, this is a mix of React Actions, Dispatcher, and Stores.

var app = app || {};

(function() {
  'use strict';

  app.Dispatcher = function() {
  };

  app.Dispatcher.prototype = {
    addList: function(name) {
      return app.Lists.insert({name: name});
    },
    editListName: function(listId, name) {
      app.Lists.update(listId, {$set: {name: name}});
    },
    addTodo: function(listId, text, tags) {
      return app.Todos.insert({
        listId: listId,
        text: text,
        done: false,
        timestamp: (new Date()).getTime(),
        tags: tags
      });
    },
    removeTodo: function(todoId) {
      app.Todos.remove(todoId);
    },
    editTodoText: function(todoId, text) {
      app.Todos.update(todoId, {$set: {text: text}});
    },
    markTodoDone: function(todoId, done) {
      app.Todos.update(todoId, {$set: {done: done}});
    },
    addTag: function(todoId, tag) {
      app.Todos.update(todoId, {$addToSet: {tags: tag}});
    },
    removeTag: function(todoId, tag) {
      app.Todos.update(todoId, {$pull: {tags: tag}});
    }
  };
}());
