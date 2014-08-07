// Note, this is a mix of React Actions, Dispatcher, and Stores.

'use strict';

module.exports = Dispatcher;

function Dispatcher(lists, todos) {
  this.lists_ = lists;
  this.todos_ = todos;
}

Dispatcher.prototype = {
  addList: function(name) {
    return this.lists_.insert({name: name});
  },
  editListName: function(listId, name) {
    this.lists_.update(listId, {$set: {name: name}});
  },
  addTodo: function(listId, text, tags) {
    return this.todos_.insert({
      listId: listId,
      text: text,
      done: false,
      timestamp: (new Date()).getTime(),
      tags: tags
    });
  },
  removeTodo: function(todoId) {
    this.todos_.remove(todoId);
  },
  editTodoText: function(todoId, text) {
    this.todos_.update(todoId, {$set: {text: text}});
  },
  markTodoDone: function(todoId, done) {
    this.todos_.update(todoId, {$set: {done: done}});
  },
  addTag: function(todoId, tag) {
    this.todos_.update(todoId, {$addToSet: {tags: tag}});
  },
  removeTag: function(todoId, tag) {
    this.todos_.update(todoId, {$pull: {tags: tag}});
  }
};
