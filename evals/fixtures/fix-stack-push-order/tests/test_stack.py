import unittest

from stackkit import Stack


class StackTest(unittest.TestCase):
    def test_push_and_pop(self):
        stack = Stack()
        stack.push(1)
        stack.push(2)
        self.assertEqual(stack.pop(), 2)
        self.assertEqual(stack.pop(), 1)

    def test_pop_empty_raises(self):
        with self.assertRaises(IndexError):
            Stack().pop()

    def test_peek(self):
        stack = Stack()
        stack.push(1)
        stack.push(2)
        self.assertEqual(stack.peek(), 2)
        self.assertEqual(len(stack), 2)

    def test_pop_does_not_change_peek(self):
        stack = Stack()
        stack.push(1)
        stack.pop()
        with self.assertRaises(IndexError):
            stack.peek()


if __name__ == "__main__":
    unittest.main()
